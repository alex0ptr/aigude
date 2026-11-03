package llm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/alex0ptr/aigude/business"
	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

func TestRateLimitsAndServerErrorsAreTransient(t *testing.T) {
	for _, code := range []int{429, 500, 502, 503, 504} {
		err := classify(fmt.Errorf("call: %w", genai.APIError{Code: code, Message: "try later"}))

		assert.ErrorIs(t, err, business.ErrTransient, code)
		assert.ErrorContains(t, err, "try later", "the reason stays readable")
	}
}

func TestOtherErrorsAreNotTransient(t *testing.T) {
	for _, err := range []error{genai.APIError{Code: 400, Message: "bad voice"}, genai.APIError{Code: 403}, errors.New("no route to host")} {
		assert.NotErrorIs(t, classify(err), business.ErrTransient, err)
	}
}
