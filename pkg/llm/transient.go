package llm

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/alex0ptr/aigude/business"
	"google.golang.org/genai"
)

// classify marks an error of the Gemini API that may go away on its own, a rate limit or an error of the server, as
// business.ErrTransient. Any other error is returned as it is.
func classify(err error) error {
	var api genai.APIError
	if errors.As(err, &api) && (api.Code == http.StatusTooManyRequests || api.Code >= http.StatusInternalServerError) {
		return fmt.Errorf("%w: %w", business.ErrTransient, err)
	}
	return err
}
