package codex

// SkillProjection uses Codex's documented repository skill root. Other runtimes
// may discover this shared directory as well.
type SkillProjection struct{}

func (SkillProjection) SkillDirectory() string { return ".agents/skills" }
