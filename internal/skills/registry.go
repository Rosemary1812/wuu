package skills

import (
	"sort"
	"strings"
)

type Registry struct {
	items []Skill
}

func NewRegistry(items []Skill) *Registry {
	out := append([]Skill(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return &Registry{items: out}
}

func (r *Registry) List() []Skill {
	if r == nil {
		return nil
	}
	out := make([]Skill, len(r.items))
	copy(out, r.items)
	return out
}

func (r *Registry) Find(name string) (Skill, bool) {
	if r == nil {
		return Skill{}, false
	}
	return Find(r.items, name)
}

func (r *Registry) Route(prompt string, touchedPaths []string) []Skill {
	if r == nil {
		return nil
	}
	prompt = strings.ToLower(prompt)
	active := FilterActiveSkills(r.items, touchedPaths)
	scored := make([]routedSkill, 0, len(active))
	for _, skill := range active {
		score := routeScore(skill, prompt)
		if score > 0 {
			scored = append(scored, routedSkill{Skill: skill, Score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Name < scored[j].Name
		}
		return scored[i].Score > scored[j].Score
	})
	out := make([]Skill, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.Skill)
	}
	return out
}

type routedSkill struct {
	Skill
	Score int
}

func routeScore(skill Skill, prompt string) int {
	if strings.TrimSpace(prompt) == "" {
		return 0
	}
	score := 0
	name := strings.ToLower(skill.Name)
	if name != "" && strings.Contains(prompt, name) {
		score += 8
	}
	for _, field := range []string{skill.TriggerCondition, skill.WhenToUse, skill.Description} {
		score += keywordOverlap(prompt, field)
	}
	for _, example := range skill.Examples {
		score += keywordOverlap(prompt, example)
	}
	return score
}

func keywordOverlap(prompt, text string) int {
	text = strings.ToLower(text)
	if strings.TrimSpace(text) == "" {
		return 0
	}
	score := 0
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_'
	}) {
		if len(token) < 4 {
			continue
		}
		if strings.Contains(prompt, token) {
			score++
		}
	}
	return score
}
