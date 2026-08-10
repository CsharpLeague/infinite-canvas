package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func CanvasSkills(w http.ResponseWriter, r *http.Request) {
	items, err := service.ListPublishedCanvasSkills(parseQuery(r))
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, items)
}

func AdminCanvasSkills(w http.ResponseWriter, r *http.Request) {
	result, err := service.ListAdminCanvasSkills(parseQuery(r))
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

func AdminSaveCanvasSkill(w http.ResponseWriter, r *http.Request) {
	var item model.CanvasSkill
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		FailError(w, err)
		return
	}
	result, err := service.SaveCanvasSkill(item)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

func AdminImportCanvasSkill(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		FailError(w, errors.New("请选择 Skill ZIP 文件"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		FailError(w, err)
		return
	}
	result, err := service.ImportCanvasSkillPackage(data)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

func AdminDeleteCanvasSkill(w http.ResponseWriter, r *http.Request, id string) {
	if err := service.DeleteCanvasSkill(id); err != nil {
		FailError(w, err)
		return
	}
	OK(w, true)
}
