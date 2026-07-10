package extensions

import "time"

type GrantScope string

const (
	GrantScopeAction  GrantScope = "action"
	GrantScopeSession GrantScope = "session"
	GrantScopeProject GrantScope = "project"
	GrantScopeUser    GrantScope = "user"
)

type Grant struct {
	SubjectID   string     `json:"subject_id"`
	Fingerprint string     `json:"fingerprint"`
	Scope       GrantScope `json:"scope"`
	Permissions []string   `json:"permissions,omitempty"`
	ApprovedAt  time.Time  `json:"approved_at"`
}

type Settings struct {
	Grants map[string]Grant `json:"grants,omitempty"`
}

func (s Settings) FindGrant(subjectID, fingerprint string) (Grant, bool) {
	grant, ok := s.Grants[subjectID]
	if !ok || grant.SubjectID != subjectID || grant.Fingerprint != fingerprint {
		return Grant{}, false
	}
	grant.Permissions = append([]string(nil), grant.Permissions...)
	return grant, true
}
