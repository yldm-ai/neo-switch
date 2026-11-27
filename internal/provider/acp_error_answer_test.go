package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/yldm-ai/neo-switch/internal/translate"
)

func TestAgyAnswerErrorClassifiesAgyErrorLine(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		want         any
	}{
		{"resource exhausted", "⚠ RESOURCE_EXHAUSTED (code 429): Resource has been exhausted (e.g. check quota).", &translate.RateLimitError{}},
		{"used-up quota", "⚠ RESOURCE_EXHAUSTED (code 429): You have exhausted your quota on this model.", &translate.UnavailableError{}},
		{"unknown 429", "⚠ THROTTLED (code 429): slow down", &translate.RateLimitError{}},
		{"server error", "\n⚠️ UNAVAILABLE (code 503): The service is currently unavailable.", &translate.RetryableError{}},
		{"bad request", "⚠ INVALID_ARGUMENT (code 400): Request contains an invalid argument.", &translate.UnavailableError{}},
		{"too long", "⚠ INVALID_ARGUMENT (code 400): The input token count exceeds the maximum", &translate.UnavailableError{}},
		{"plain warning", "⚠ Careful: this deletes files.", nil},
		{"envelope", `{"text":"⚠ RESOURCE_EXHAUSTED (code 429): quoted"}`, nil},
		{"quoted later", "Antigravity said ⚠ RESOURCE_EXHAUSTED (code 429)", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := agyAnswerError(tc.answer, DefaultModel)
			switch want := tc.want.(type) {
			case nil:
				if err != nil {
					t.Fatalf("got %v, want no error", err)
				}
			case *translate.RateLimitError:
				if !errors.As(err, &want) || !want.Retried {
					t.Fatalf("got %T %+v, want a rate limit agy already retried", err, err)
				}
			case *translate.RetryableError:
				if !errors.As(err, &want) {
					t.Fatalf("got %T %v, want retryable", err, err)
				}
			case *translate.UnavailableError:
				if !errors.As(err, &want) {
					t.Fatalf("got %T %v, want unavailable", err, err)
				}
			}
		})
	}
}

// agy sends Google's refusal as the answer of a normally ended turn. It must reach Claude Code as a 429 it retries, never as the model's words.
func TestACPAgyErrorAnswerBecomesRateLimit(t *testing.T) {
	a := newFakeACP(t, "agy-error-429")
	var rateLimited *translate.RateLimitError
	if _, err := a.Generate(context.Background(), textRequest("hi")); !errors.As(err, &rateLimited) {
		t.Fatalf("Generate: got %T %v, want a rate limit", err, err)
	}
	var chunks int
	var streamErr error
	for resp, err := range a.Stream(context.Background(), textRequest("hi")) {
		if err != nil {
			streamErr = err
			break
		}
		if resp != nil {
			chunks++
		}
	}
	if chunks != 0 {
		t.Fatalf("Stream yielded %d chunks before the error; the error line must not be shown as text", chunks)
	}
	if !errors.As(streamErr, &rateLimited) {
		t.Fatalf("Stream: got %T %v, want a rate limit", streamErr, streamErr)
	}
}

// A model answer that merely starts with the warning sign is still delivered, only not streamed early.
func TestACPWarningTextIsStillAnswered(t *testing.T) {
	a := newFakeACP(t, "warning-text")
	var text string
	for resp, err := range a.Stream(context.Background(), textRequest("hi")) {
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range resp.Candidates {
			if c.Content == nil {
				continue
			}
			for _, p := range c.Content.Parts {
				text += p.Text
			}
		}
	}
	if text != "⚠ This answer ignored the envelope." {
		t.Fatalf("text = %q", text)
	}
}
