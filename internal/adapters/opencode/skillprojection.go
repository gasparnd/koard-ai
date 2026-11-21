package opencode

// SkillProjection describes project-native discovery, not the proposal runner's
// skill permissions, which remain unchanged.
type SkillProjection struct{}

func (SkillProjection) SkillDirectory() string { return ".opencode/skills" }
