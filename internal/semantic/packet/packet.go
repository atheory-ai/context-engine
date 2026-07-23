// Package packet turns a decorated semantic plan into the compact, auditable
// contract handed to an implementation LLM or harness agent. It never renders
// source; code generation remains the responsibility of that agent.
package packet

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atheory-ai/context-engine/internal/iir"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
)

const SchemaVersionV1 = "v1"

type Status string

const (
	StatusReady   Status = "ready"
	StatusBlocked Status = "blocked"
)

// Requirement is a compact referenceable obligation for an implementation
// agent. Evidence IDs let the full SemanticPlan remain the source of detail.
type Requirement struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Requirement  string   `json:"requirement"`
	Producer     string   `json:"producer"`
	EvidenceRefs []string `json:"evidenceRefs"`
}

// ImplementationPacket is the only source-generation input CE needs to hand
// to an LLM. It is intentionally semantic rather than a source template.
type ImplementationPacket struct {
	SchemaVersion  string               `json:"schemaVersion"`
	PlanRevisionID string               `json:"planRevisionId"`
	Status         Status               `json:"status"`
	Target         plan.SemanticUnit    `json:"target"`
	Intent         *iir.FunctionIntent  `json:"intent"`
	Bindings       []plan.SymbolBinding `json:"bindings"`
	Claims         []plan.SemanticClaim `json:"claims"`
	Requirements   []Requirement        `json:"requirements"`
	OpenQuestions  []plan.OpenQuestion  `json:"openQuestions"`
	Instructions   []string             `json:"instructions"`
}

// Build creates a deterministic implementation packet. A blocked packet is
// useful for an agent to answer focused questions, but must not be used to
// produce source until its blocking questions are resolved.
func Build(source *plan.SemanticPlan) (*ImplementationPacket, error) {
	if source == nil {
		return nil, fmt.Errorf("implementation packet: plan is required")
	}
	if err := source.Validate(); err != nil {
		return nil, fmt.Errorf("implementation packet: %w", err)
	}
	requirements := make([]Requirement, 0, len(source.Obligations))
	for _, obligation := range source.Obligations {
		requirements = append(requirements, Requirement{
			ID: obligation.ID, Kind: obligation.Kind, Requirement: obligation.Requirement,
			Producer: producer(obligation.Evidence), EvidenceRefs: evidenceIDs(obligation.Evidence),
		})
	}
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].ID < requirements[j].ID })
	status := StatusReady
	if source.Lifecycle == plan.LifecycleBlocked || hasBlockingQuestions(source.OpenQuestions) {
		status = StatusBlocked
	}
	return &ImplementationPacket{
		SchemaVersion:  SchemaVersionV1,
		PlanRevisionID: source.ID,
		Status:         status,
		Target:         source.Unit,
		Intent:         source.Intent,
		Bindings:       append([]plan.SymbolBinding(nil), source.Bindings...),
		Claims:         append([]plan.SemanticClaim(nil), source.Claims...),
		Requirements:   requirements,
		OpenQuestions:  append([]plan.OpenQuestion(nil), source.OpenQuestions...),
		Instructions:   instructions(status),
	}, nil
}

// Prompt renders only the contract and explicit instructions an implementation
// agent needs. It deliberately avoids an opaque plugin prompt fragment.
func Prompt(p *ImplementationPacket) (string, error) {
	if p == nil || p.Intent == nil {
		return "", fmt.Errorf("implementation packet prompt: packet and intent are required")
	}
	var b strings.Builder
	b.WriteString("Implement the following Context Engine semantic contract.\n")
	b.WriteString("Do not weaken, omit, or invent requirements. Cite a blocking question instead of guessing.\n\n")
	fmt.Fprintf(&b, "Plan revision: %s\nTarget language: %s\nTarget: %s\nFunction: %s\n", p.PlanRevisionID, p.Target.Language, p.Target.CanonicalID, p.Intent.Name)
	if len(p.Requirements) > 0 {
		b.WriteString("\nMandatory requirements:\n")
		for _, requirement := range p.Requirements {
			fmt.Fprintf(&b, "- [%s] %s (%s; %s)\n", requirement.ID, requirement.Requirement, requirement.Kind, requirement.Producer)
		}
	}
	if len(p.OpenQuestions) > 0 {
		b.WriteString("\nOpen questions:\n")
		for _, question := range p.OpenQuestions {
			fmt.Fprintf(&b, "- [%s] %s\n", question.ID, question.Prompt)
		}
	}
	if p.Status == StatusBlocked {
		b.WriteString("\nDo not write source until the blocking questions are answered.\n")
	}
	return b.String(), nil
}

func hasBlockingQuestions(questions []plan.OpenQuestion) bool {
	for _, question := range questions {
		if question.Blocking {
			return true
		}
	}
	return false
}

func evidenceIDs(evidence []plan.Evidence) []string {
	ids := make([]string, 0, len(evidence))
	for _, item := range evidence {
		ids = append(ids, item.ID)
	}
	return ids
}

func producer(evidence []plan.Evidence) string {
	if len(evidence) == 0 {
		return "unknown"
	}
	return evidence[0].Producer
}

func instructions(status Status) []string {
	base := []string{
		"Treat requirements as mandatory unless the plan explicitly says otherwise.",
		"Use resolved bindings and cited evidence; do not rediscover or replace them from guesswork.",
		"Return a proposed patch and explain how each requirement is satisfied.",
	}
	if status == StatusBlocked {
		return append(base, "Ask the listed blocking questions before proposing source changes.")
	}
	return base
}
