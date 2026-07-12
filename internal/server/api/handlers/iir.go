package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/atheory-ai/context-engine/internal/iir"
)

// maxIIRBodyBytes caps IIR request bodies before decoding, so an oversized
// payload can't consume host memory or feed unbounded input to tree-sitter.
// Matches the ce.iir_* host-function payload cap (4 MiB).
const maxIIRBodyBytes = 4 << 20

// readJSONBody size-limits and decodes an IIR request body, writing the
// appropriate error response (413 when too large, 400 when malformed) and
// returning ok=false on failure.
func readJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxIIRBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

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
// rulePack supplies the effective rule pack (built-in defaults merged with any
// plugin-contributed rules); it is evaluated per request so newly loaded plugins
// take effect.
func IIRVerify(extractor iir.Extractor, rulePack func() iir.RulePack) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req iirVerifyRequest
		if !readJSONBody(w, r, &req) {
			return
		}
		intent, err := iir.ParseIntentJSON(req.Intent)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		report, err := iir.VerifySource(r.Context(), extractor, intent, []byte(req.Source), rulePack())
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
	if !readJSONBody(w, r, &req) {
		return nil, false
	}
	intent, err := iir.ParseIntentJSON(req.Intent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return intent, true
}
