package confluencetest

import (
	"encoding/json"
	"net/http"
	"slices"
)

// fakeContentStatus is a status fixture shared by the list and page endpoints.
type fakeContentStatus struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

var (
	spaceContentStatuses    = []fakeContentStatus{{ID: 0, Name: "Ready for review"}, {ID: 1, Name: "In progress"}}
	personalContentStatuses = []fakeContentStatus{{ID: 2, Name: "Needs copy edit"}}
)

// contentStatus serves the v1 Cloud endpoints for the fake's page statuses.
func (s *Server) contentStatus(w http.ResponseWriter, r *http.Request, id string, available bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page := s.pages[id]
	if s.dataCenter || page == nil || page.Trashed || page.Type != "page" {
		http.NotFound(w, r)
		return
	}

	if available {
		writeJSON(w, http.StatusOK, map[string]any{
			"spaceContentStates": spaceContentStatuses, "customContentStates": personalContentStatuses,
		})
		return
	}
	states := slices.Concat(spaceContentStatuses, personalContentStatuses)

	if r.URL.Query().Get("status") != "current" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "expected current status"})
		return
	}

	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var payload struct {
			ID *int64 `json:"id"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil || payload.ID == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "expected a status ID"})
			return
		}
		for _, candidate := range states {
			if candidate.ID == *payload.ID {
				page.ContentStatus = candidate.Name
				page.Version++
				page.Message = ""
				writeJSON(w, http.StatusOK, map[string]any{"contentState": candidate})
				return
			}
		}
		http.NotFound(w, r)
		return
	case http.MethodDelete:
		page.ContentStatus = ""
		page.Version++
		page.Message = ""
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	for _, candidate := range states {
		if candidate.Name == page.ContentStatus {
			writeJSON(w, http.StatusOK, map[string]any{"contentState": candidate})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}
