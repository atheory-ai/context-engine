package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/atheory-ai/context-engine/internal/iir"
)

// IIR handlers expose the Intermediate Intent Representation over HTTP. They are
// pure computations over internal/iir and need no engine — verification and
// generation don't touch the substrate. The RFC's "intent → code on every
// surface": CLI (built) + MCP + this API.

// iirVerifyRequest is the body of POST /api/v1/iir/verify.
type iirVerifyRequest struct {
	Intent json.RawMessage `json:"intent"` // a FunctionIntent object
	Source string          `json:"source"`
}

// iirGenerateRequest / iirGenTestsRequest are the bodies of the generation
// endpoints — just the intended IIR.
type iirIntentRequest struct {
	Intent json.RawMessage `json:"intent"`
}

// IIRVerify handles POST /api/v1/iir/verify — verify source against intended IIR.
func IIRVerify() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req iirVerifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		intent, err := iir.ParseIntentJSON(req.Intent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		report, err := iir.VerifySource(r.Context(), intent, []byte(req.Source), iir.DefaultRulePack())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

// IIRGenerate handles POST /api/v1/iir/generate — emit source from intended IIR.
func IIRGenerate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		intent, ok := decodeIntent(w, r)
		if !ok {
			return
		}
		source, err := iir.GenerateFunction(intent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"source": source})
	}
}

// IIRGenTests handles POST /api/v1/iir/gen-tests — emit tests from intended IIR.
func IIRGenTests() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		intent, ok := decodeIntent(w, r)
		if !ok {
			return
		}
		artifact, err := iir.GenerateTests(intent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, artifact)
	}
}

// decodeIntent reads and parses an {"intent": {...}} body, writing an error
// response and returning ok=false on failure.
func decodeIntent(w http.ResponseWriter, r *http.Request) (*iir.FunctionIntent, bool) {
	var req iirIntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	intent, err := iir.ParseIntentJSON(req.Intent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return intent, true
}
