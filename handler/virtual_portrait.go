package handler

import (
	"encoding/json"
	"net/http"

	"github.com/tigerowo/infinite-canvas/service"
)

func CreateVirtualPortrait(w http.ResponseWriter, r *http.Request) {
	var input service.VirtualPortraitCreateInput
	_ = json.NewDecoder(r.Body).Decode(&input)
	result, err := service.CreateVirtualPortrait(r.Context(), input)
	if err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, result)
}

func GetVirtualPortrait(w http.ResponseWriter, r *http.Request, id string) {
	result, err := service.RefreshVirtualPortrait(r.Context(), id)
	if err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, result)
}

func DeleteVirtualPortrait(w http.ResponseWriter, r *http.Request, id string) {
	if err := service.DeleteVirtualPortrait(r.Context(), id); err != nil {
		Fail(w, err.Error())
		return
	}
	OK(w, true)
}
