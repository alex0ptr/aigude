package service_test

import (
	"net/http"
	"testing"

	"github.com/alex0ptr/aigude/pkg/service/servicetest"
	"github.com/stretchr/testify/assert"
)

func TestHealthEndpointAnswersOkWhateverTheProxyHeaderSays(t *testing.T) {
	h := servicetest.Start(t, threeUsers()...)
	tests := map[string]map[string]string{
		"no header":         nil,
		"unknown User":      pinned("nobody"),
		"empty User":        pinned(""),
		"known pinned User": pinned("alex"),
		"demo User":         visitor(),
	}
	for name, headers := range tests {
		t.Run(name, func(t *testing.T) {
			page := h.GetWithHeaders(t, "/healthz", headers)

			assert.Equal(t, http.StatusOK, page.Status)
			assert.Equal(t, "ok", page.Body)
		})
	}
}
