package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

type VirtualPortraitCreateInput struct {
	ChannelID string `json:"channelId"`
	SourceURL string `json:"sourceUrl"`
	Name      string `json:"name"`
}

func CreateVirtualPortrait(ctx context.Context, input VirtualPortraitCreateInput) (model.VirtualPortraitTask, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return model.VirtualPortraitTask{}, errors.New("请先登录")
	}
	channel, err := virtualPortraitChannel(input.ChannelID)
	if err != nil {
		return model.VirtualPortraitTask{}, err
	}
	sourceURL, err := resolveVirtualPortraitSourceURL(user.ID, input.SourceURL)
	if err != nil {
		return model.VirtualPortraitTask{}, err
	}
	fingerprintSum := sha256.Sum256([]byte(sourceURL))
	fingerprint := hex.EncodeToString(fingerprintSum[:])
	existing, found, findErr := repository.FindVirtualPortraitTask(user.ID, channel.ID, fingerprint)
	if findErr != nil {
		return model.VirtualPortraitTask{}, findErr
	}
	if found && (existing.Status == "processing" || existing.Status == "active") {
		return existing, nil
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "虚拟人像"
	}
	groupID, err := callArkAssetCreate(channel, "CreateAssetGroup", map[string]any{"Name": name, "Description": "无限画布虚拟人像", "GroupType": "AIGC", "ProjectName": "default"})
	if err != nil {
		return model.VirtualPortraitTask{}, err
	}
	assetID, err := callArkAssetCreate(channel, "CreateAsset", map[string]any{"GroupId": groupID, "URL": sourceURL, "Name": name, "AssetType": "Image", "ProjectName": "default"})
	now := time.Now().Format(time.RFC3339)
	taskID, createdAt := uuid.NewString(), now
	if found {
		taskID, createdAt = existing.ID, existing.CreatedAt
	}
	task := model.VirtualPortraitTask{ID: taskID, UserID: user.ID, ChannelID: channel.ID, SourceFingerprint: fingerprint, SourceURL: sourceURL, Name: name, GroupID: groupID, AssetID: assetID, Status: "processing", CreatedAt: createdAt, UpdatedAt: now}
	if err != nil {
		task.Status, task.Error = "failed", friendlyVirtualPortraitError(err.Error())
	}
	if saveErr := repository.SaveVirtualPortraitTask(task); saveErr != nil {
		return task, saveErr
	}
	return task, nil
}

func resolveVirtualPortraitSourceURL(userID string, value string) (string, error) {
	sourceURL := strings.TrimSpace(value)
	parsed, err := url.Parse(sourceURL)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return sourceURL, nil
	}
	path := strings.Trim(strings.TrimPrefix(sourceURL, "server:"), "/")
	parts := strings.Split(path, "/")
	objectID := ""
	if strings.HasPrefix(sourceURL, "server:") {
		objectID = path
	} else if len(parts) >= 3 && parts[0] == "api" && parts[1] == "files" {
		objectID = parts[2]
	}
	if objectID == "" {
		return "", errors.New("虚拟人像必须使用已上传云存储的公网图片")
	}
	object, err := repository.GetStorageObject(objectID)
	if err != nil {
		return "", errors.New("没有找到虚拟人像对应的云存储图片")
	}
	if object.CreatedBy != "" && object.CreatedBy != userID {
		return "", errors.New("无权使用该云存储图片创建虚拟人像")
	}
	publicURL := strings.TrimSpace(object.PublicURL)
	parsed, err = url.Parse(publicURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("该图片没有配置可供火山访问的云存储公网地址")
	}
	return publicURL, nil
}

func RefreshVirtualPortrait(ctx context.Context, id string) (model.VirtualPortraitTask, error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return model.VirtualPortraitTask{}, errors.New("请先登录")
	}
	task, found, err := repository.GetVirtualPortraitTask(user.ID, id)
	if err != nil || !found {
		return task, errors.New("虚拟人像记录不存在")
	}
	if task.Status != "processing" {
		return task, nil
	}
	channel, err := virtualPortraitChannel(task.ChannelID)
	if err != nil {
		return task, err
	}
	result, err := callArkAsset(channel, "GetAsset", map[string]any{"Id": task.AssetID, "ProjectName": "default"})
	if err != nil {
		return task, err
	}
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(result["Status"])))
	switch status {
	case "active":
		task.Status = "active"
		task.Error = ""
	case "failed":
		task.Status = "failed"
		task.Error = friendlyVirtualPortraitError(readAssetError(result))
	default:
		task.Status = "processing"
	}
	task.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := repository.SaveVirtualPortraitTask(task); err != nil {
		return task, err
	}
	return task, nil
}

func DeleteVirtualPortrait(ctx context.Context, id string) error {
	user, ok := UserFromContext(ctx)
	if !ok {
		return errors.New("请先登录")
	}
	task, found, err := repository.GetVirtualPortraitTask(user.ID, id)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if strings.TrimSpace(task.AssetID) != "" {
		channel, channelErr := virtualPortraitChannel(task.ChannelID)
		if channelErr != nil {
			return channelErr
		}
		if _, deleteErr := callArkAsset(channel, "DeleteAsset", map[string]any{"Id": task.AssetID, "ProjectName": "default"}); deleteErr != nil {
			return fmt.Errorf("删除火山虚拟人像失败：%w", deleteErr)
		}
	}
	return repository.DeleteVirtualPortraitTask(user.ID, task.ID)
}

func virtualPortraitChannel(id string) (model.ModelChannel, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return model.ModelChannel{}, err
	}
	for _, channel := range settings.Private.Channels {
		if channel.ID == id && channel.Enabled && strings.EqualFold(channel.Protocol, "ark") && strings.TrimSpace(channel.AssetAccessKeyID) != "" && strings.TrimSpace(channel.AssetSecretAccessKey) != "" {
			return channel, nil
		}
	}
	return model.ModelChannel{}, errors.New("当前火山渠道未配置虚拟人像素材鉴权")
}

func callArkAssetCreate(channel model.ModelChannel, action string, body map[string]any) (string, error) {
	result, err := callArkAsset(channel, action, body)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(fmt.Sprint(result["Id"]))
	if id == "" || id == "<nil>" {
		return "", errors.New("火山素材接口没有返回 ID")
	}
	return id, nil
}

func callArkAsset(channel model.ModelChannel, action string, body map[string]any) (map[string]any, error) {
	payload, _ := json.Marshal(body)
	query := url.Values{"Action": {action}, "Version": {"2024-01-01"}}
	request, _ := http.NewRequest(http.MethodPost, "https://ark.cn-beijing.volcengineapi.com/?"+query.Encode(), bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	signArkAssetRequest(request, payload, channel.AssetAccessKeyID, channel.AssetSecretAccessKey)
	response, err := adminModelHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(response.Body)
	var envelope struct {
		Result           map[string]any `json:"Result"`
		Error            map[string]any `json:"Error"`
		ResponseMetadata map[string]any `json:"ResponseMetadata"`
	}
	if json.Unmarshal(responseBody, &envelope) != nil {
		return nil, errors.New("火山素材接口返回了无法解析的数据")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Result == nil {
		message := ""
		if envelope.Error != nil {
			message = strings.TrimSpace(fmt.Sprint(envelope.Error["Message"]))
		}
		if message == "" && envelope.ResponseMetadata != nil {
			message = strings.TrimSpace(fmt.Sprint(envelope.ResponseMetadata["Error"]))
		}
		return nil, errors.New(firstNonEmpty(message, string(responseBody), "火山素材接口请求失败"))
	}
	return envelope.Result, nil
}

func signArkAssetRequest(request *http.Request, payload []byte, accessKeyID, secretAccessKey string) {
	now := time.Now().UTC()
	xDate, date := now.Format("20060102T150405Z"), now.Format("20060102")
	payloadHash := sha256Text(payload)
	request.Header.Set("Host", request.URL.Host)
	request.Header.Set("X-Date", xDate)
	request.Header.Set("X-Content-Sha256", payloadHash)
	signedHeaderNames := []string{"content-type", "host", "x-content-sha256", "x-date"}
	sort.Strings(signedHeaderNames)
	canonicalHeaders := ""
	for _, name := range signedHeaderNames {
		value := request.Header.Get(name)
		if name == "host" {
			value = request.URL.Host
		}
		canonicalHeaders += name + ":" + strings.TrimSpace(value) + "\n"
	}
	signedHeaders := strings.Join(signedHeaderNames, ";")
	canonicalRequest := request.Method + "\n/\n" + request.URL.RawQuery + "\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash
	scope := date + "/cn-beijing/ark/request"
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + sha256Text([]byte(canonicalRequest))
	kDate := arkHMAC([]byte(secretAccessKey), date)
	kRegion := arkHMAC(kDate, "cn-beijing")
	kService := arkHMAC(kRegion, "ark")
	signature := hex.EncodeToString(arkHMAC(arkHMAC(kService, "request"), stringToSign))
	request.Header.Set("Authorization", "HMAC-SHA256 Credential="+accessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func arkHMAC(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Text(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func readAssetError(result map[string]any) string {
	if value, ok := result["Error"].(map[string]any); ok {
		return firstNonEmpty(fmt.Sprint(value["Message"]), fmt.Sprint(value["Code"]))
	}
	return "虚拟人像审核未通过"
}

func friendlyVirtualPortraitError(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "sensitive"):
		return "图片内容安全审核未通过，请更换符合要求的虚拟人物图片"
	case strings.Contains(lower, "format"):
		return "无法识别图片格式，请使用 JPG、PNG 或 WebP 图片"
	case strings.Contains(lower, "face"):
		return "未识别到清晰完整的人像，请使用正面、无遮挡的虚拟人物图片"
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden"):
		return "素材资产鉴权失败，请检查火山渠道的素材 API Key 与权益"
	default:
		return firstNonEmpty(strings.TrimSpace(message), "虚拟人像入库失败")
	}
}
