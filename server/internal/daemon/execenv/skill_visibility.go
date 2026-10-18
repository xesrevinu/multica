package execenv

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// modelVisibleSkills returns the skills a model may invoke, with Name
// normalized to the on-disk slug.
//
// The normalization is not cosmetic. The runtime brief renders these entries
// into the listing the model reads to pick a skill, and the only name it can
// actually invoke is the directory slug writeSkillFiles lays down —
// sanitizeSkillName of the same field. A workspace skill's Name is its human
// display name ("PR review"), so listing it verbatim hands the model an
// identifier that does not resolve (MUL-5529).
//
// Slugs come from resolveSkillSlugs over the *unfiltered* batch, because
// writeSkillFiles resolves slugs over every skill — including the ones hidden
// here — and a hidden skill that claims a slug also denies it to the later
// claimant. Filtering first would make the listing disagree with the
// directories on disk.
//
// A skill that loses a batch collision has no directory at all, so it is
// omitted from the listing rather than advertised under a name nothing
// answers to.
func modelVisibleSkills(skills []SkillContextForEnv) []SkillContextForEnv {
	if len(skills) == 0 {
		return nil
	}
	slugs := resolveSkillSlugs(skills)
	visible := make([]SkillContextForEnv, 0, len(skills))
	for i, skill := range skills {
		if slugs[i] == "" || !skillModelInvocationVisible(skill) {
			continue
		}
		skill.Name = slugs[i]
		visible = append(visible, skill)
	}
	return visible
}

// resolveSkillSlugs assigns each skill in a batch its on-disk directory slug,
// and "" to the skills that get no directory at all.
//
// sanitizeSkillName is not injective: "A B" and "A-B" both reduce to "a-b",
// and one directory cannot hold two skills. The first claimant in batch order
// keeps the slug and every later claimant is dropped — dropped rather than
// renamed, because a suffixed directory ("a-b-multica") is a second name for
// the same skill that nothing else in the system knows about. writeSkillFiles
// applies this same function, so the listing and the directories stay in step;
// it is deterministic in the batch (index order only), so the brief stays
// byte-identical across runs for identical input.
func resolveSkillSlugs(skills []SkillContextForEnv) []string {
	slugs := make([]string, len(skills))
	taken := make(map[string]struct{}, len(skills))
	for i, skill := range skills {
		base := sanitizeSkillName(skill.Name)
		if _, clash := taken[base]; clash {
			continue
		}
		taken[base] = struct{}{}
		slugs[i] = base
	}
	return slugs
}

func skillModelInvocationVisible(skill SkillContextForEnv) bool {
	return !skillDisablesModelInvocation(skill.Content)
}

func skillDisablesModelInvocation(content string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok || strings.TrimSpace(fmBody) == "" {
		return false
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(fmBody), &data); err != nil {
		return false
	}
	value, ok := data["disable-model-invocation"]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}
