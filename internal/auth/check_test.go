package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testChecker(fn roundTripperFunc) *Checker {
	return &Checker{
		Client:        &http.Client{Transport: fn},
		OpenAIBaseURL: "https://openai.test",
		ClaudeBaseURL: "https://claude.test",
		GeminiBaseURL: "https://gemini.test",
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestValidateOpenAIRejectsBadFormat(t *testing.T) {
	res := testChecker(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("http should not be called for bad format")
		return nil, nil
	}).Validate(context.Background(), "openai", "bad-key")
	if res.OK || !strings.Contains(res.Message, "start with sk-") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestValidateClaudeRequiresAdminKey(t *testing.T) {
	res := testChecker(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("http should not be called for bad format")
		return nil, nil
	}).Validate(context.Background(), "claude", "sk-ant-api-123")
	if res.OK || !strings.Contains(res.Message, "sk-ant-admin") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestValidateGeminiReturnsWarning(t *testing.T) {
	res := testChecker(func(r *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"models":[]}`), nil
	}).Validate(context.Background(), "gemini", "AIza123")
	if !res.OK || !strings.Contains(res.Warning, "N/A") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestValidateOpenAIAdminEndpoint(t *testing.T) {
	res := testChecker(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "/v1/organization/costs") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return response(http.StatusForbidden, `{"error":"forbidden"}`), nil
	}).Validate(context.Background(), "openai", "sk-test-123")
	if res.OK || !strings.Contains(res.Message, "Admin API key") {
		t.Fatalf("unexpected result: %+v", res)
	}
}
