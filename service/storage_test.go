package service

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tigerowo/infinite-canvas/model"
)

func TestNewS3RequestUsesTOSVirtualHostStyle(t *testing.T) {
	provider := model.StorageProvider{
		Endpoint:        "https://tos-cn-beijing.volces.com",
		Region:          "cn-beijing",
		Bucket:          "seedcan",
		AccessKeyID:     "test-ak",
		SecretAccessKey: "test-sk",
	}
	request, err := newS3RequestWithQuery(http.MethodGet, provider, "", url.Values{"list-type": {"2"}}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := request.URL.String(), "https://seedcan.tos-s3-cn-beijing.volces.com/?list-type=2"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if !strings.Contains(request.Header.Get("Authorization"), "/cn-beijing/s3/aws4_request") {
		t.Fatalf("unexpected authorization: %s", request.Header.Get("Authorization"))
	}
}

func TestNewS3RequestKeepsPathStyleForGenericS3(t *testing.T) {
	provider := model.StorageProvider{
		Endpoint:        "https://s3.example.com",
		Region:          "auto",
		Bucket:          "assets",
		AccessKeyID:     "test-ak",
		SecretAccessKey: "test-sk",
	}
	request, err := newS3Request(http.MethodPut, provider, "canvas/image.png", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := request.URL.String(), "https://s3.example.com/assets/canvas/image.png"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}
