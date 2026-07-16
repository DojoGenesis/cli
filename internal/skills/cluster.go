// Package skills provides semantic clustering for skill categorisation.
//
// Skills sourced from the gateway often have an empty Category field because
// SKILL.md files carry no explicit category frontmatter.  ClusterCategory
// infers a category from the skill name and description using a weighted
// keyword taxonomy derived from the full Dojo / CoworkPlugins skill corpus.
//
// Design principles:
//
//   - Name tokens carry 3× the weight of description tokens.  A skill named
//     "marketing-ops" is a marketing skill even if its description mentions
//     "agent" in passing.
//   - Phrases outrank single words.  "smart contract" is a stronger blockchain
//     signal than "contract" alone.
//   - A minimum threshold of 3 prevents noise matches — skills that score
//     below the threshold fall back to "general".
//   - Categories are checked in specificity order so a tie between a precise
//     category (blockchain) and a broad one (engineering) resolves to the
//     precise one.
package skills

import (
	"strings"

	"github.com/DojoGenesis/cli/internal/client"
)

// minScore is the minimum total weighted score a category must reach before
// it is preferred over "general".
const minScore = 3

// kw pairs a keyword phrase with its relevance weight (1–3).
type kw struct {
	phrase string
	weight int
}

func k(phrase string, weight int) kw { return kw{phrase, weight} }

// category bundles a display name with its weighted keyword signals.
type category struct {
	name  string
	terms []kw
}

// taxonomy is ordered from most-specific to most-general so that ties
// resolve toward the narrower category.
var taxonomy = []category{
	// ── Blockchain & Web3 ──────────────────────────────────────────────────────
	{
		name: "blockchain",
		terms: []kw{
			k("smart contract", 3), k("blockchain", 3), k("solana", 3),
			k("ethereum", 3), k("defi", 3), k("algorand", 3), k("cairo smart", 3),
			k("cosmos sdk", 3), k("substrate pallet", 3), k("ton contract", 3),
			k("web3", 3), k("starknet", 3), k("polkadot", 3), k("evm", 3),
			k("vulnerability scanner", 2),
		},
	},

	// ── Pure Mathematics ───────────────────────────────────────────────────────
	{
		name: "math",
		terms: []kw{
			k("topology", 3), k("hilbert space", 3), k("banach space", 3),
			k("lebesgue", 3), k("theorem prov", 3), k("real analysis", 3),
			k("complex analysis", 3), k("measure theory", 3), k("category theory", 3),
			k("mathematical logic", 3), k("eigenvalue", 3), k("modular arithmetic", 3),
			k("prime number", 3), k("analytic function", 3), k("contour integral", 3),
			k("linear algebra", 3), k("abstract algebra", 3), k("vector space", 3),
			k("differential equat", 3), k("propositional logic", 3), k("predicate logic", 3),
			k("proof theory", 3), k("sigma algebra", 3), k("banach", 3),
			k("compactness", 3), k("connectedness", 3), k("convergence", 3),
			k("continuity in", 3), k("rudin", 3), k("mathlib", 3),
		},
	},

	// ── MCP / Tool Protocol ────────────────────────────────────────────────────
	{
		name: "mcp",
		terms: []kw{
			k("model context protocol", 3), k("mcp server", 3), k("mcp tool", 3),
			k("mcp builder", 3), k("fastmcp", 3), k("mcp client", 3),
			k("mcp script", 3), k("mcp chaining", 3),
		},
	},

	// ── Skill System (meta / CAS / dojo-internal) ──────────────────────────────
	{
		name: "skill-system",
		terms: []kw{
			k("skill.md", 3), k("cas distribution", 3), k("content-addressable", 3),
			k("meta-skill", 3), k("skill creator", 3), k("skill creation", 3),
			k("normalize skill", 3), k("skill audit", 3), k("skill package", 3),
			k("skill quality", 3), k("skill.md file", 3), k("writing skills", 3),
			k("skill developer", 3), k("skill-tester", 3), k("skill upgrade", 3),
			k("dojoregistry", 3), k("skill share", 3),
		},
	},

	// ── Memory & Continuity ────────────────────────────────────────────────────
	{
		name: "memory",
		terms: []kw{
			k("memory garden", 3), k("seed extraction", 3), k("auto-memory", 3),
			k("compression ritual", 3), k("continuity ledger", 3),
			k("memory artifact", 3), k("session compression", 3),
			k("memory entry", 3), k("plant a seed", 3), k("memory seed", 3),
			k("harvest", 2), k("garden", 2),
		},
	},

	// ── AI / Machine Learning ──────────────────────────────────────────────────
	{
		name: "ai-ml",
		terms: []kw{
			k("machine learning", 3), k("large language model", 3), k("llm cost", 3),
			k("rag pipeline", 3), k("rag architect", 3), k("embedding", 3),
			k("neural network", 3), k("mlops", 3), k("model training", 3),
			k("prompt engineer", 3), k("fine-tun", 3), k("semantic search", 3),
			k("llm parameter", 3), k("vector index", 3), k("computer vision", 3),
			k("ai model", 2), k("inference", 2), k("llm tuning", 3),
		},
	},

	// ── Cloud & Infrastructure ─────────────────────────────────────────────────
	{
		name: "cloud",
		terms: []kw{
			k("aws bedrock", 3), k("aws cdk", 3), k("aws serverless", 3),
			k("aws solution", 3), k(" gcp ", 3), k("azure", 3),
			k("kubernetes", 3), k("terraform", 3), k("serverless", 3),
			k("helm chart", 3), k("cloud architect", 3), k("infrastructure as code", 3),
			k("cloudflare worker", 3), k(" iac ", 3), k("docker", 2),
			k("devops", 2), k("devcontainer", 3), k(" aws ", 3), k("ci/cd", 2),
			k("lambda", 2), k("s3 bucket", 3), k("cloudformation", 3),
		},
	},

	// ── Regulatory & Compliance ────────────────────────────────────────────────
	// Listed before security: compliance is narrower — ties resolve to compliance.
	{
		name: "compliance",
		terms: []kw{
			k("iso 27001", 3), k("iso 13485", 3), k("fda", 3), k("soc 2", 3),
			k("hipaa", 3), k("gdpr", 3), k("mdr", 3), k("regulatory affair", 3),
			k("isms", 3), k("qms", 3), k(" capa ", 3), k("510(k)", 3),
			k("compliance audit", 3), k("medical device", 3), k(" nda ", 3),
			k("contract review", 3), k("legal review", 3), k(" sox ", 3),
			k("14971", 3), k("ce marking", 3),
		},
	},

	// ── Security ───────────────────────────────────────────────────────────────
	{
		name: "security",
		terms: []kw{
			k("vulnerability", 3), k("penetration test", 3), k("pentest", 3),
			k("exploit", 3), k("fuzzing", 3), k("owasp", 3), k("red team", 3),
			k("security audit", 3), k("cryptograph", 3), k("insecure default", 3),
			k("injection", 2), k("ciso", 3), k("secops", 3), k("threat model", 3),
			k("side-channel", 3), k("timing attack", 3), k("supply chain risk", 3),
			k("semgrep", 3), k("trailmark", 3), k("code audit", 3), k("asm", 2),
			k("zeroize", 3), k("constant time", 3), k("memory safety", 3),
			k("ai security", 3), k("prompt injection", 3), k("jailbreak", 3),
			k("authentication", 2), k("authorization", 2), k("rbac", 3),
			k("zero trust", 3),
		},
	},

	// ── Finance ────────────────────────────────────────────────────────────────
	{
		name: "finance",
		terms: []kw{
			k("saas metrics", 3), k("financial model", 3), k("dcf valuation", 3),
			k("fundrais", 3), k("investor", 3), k("arr", 2), k("mrr", 2),
			k("ltv", 3), k("cac", 3), k("unit economics", 3), k("cash flow", 3),
			k("budget", 3), k("cfo", 3), k("financial analyst", 3),
			k("revenue forecast", 3), k("financial ratio", 3), k("p&l", 3),
			k("saas health", 3), k("stock analyz", 3), k("stock analyzer", 3),
		},
	},

	// ── Strategy & Executive ───────────────────────────────────────────────────
	{
		name: "strategy",
		terms: []kw{
			k("ceo", 3), k("c-suite", 3), k("board meeting", 3), k("executive", 2),
			k("strategic", 3), k("competitive intel", 3), k("market expansion", 3),
			k("go-to-market", 3), k("m&a", 3), k("okr cascade", 3),
			k("positioning statement", 3), k("scenario war room", 3),
			k("business strategy", 3), k("chief of staff", 3), k("coo", 3),
			k("cto advisor", 3), k("business model", 2), k("advisor", 2),
		},
	},

	// ── Marketing ──────────────────────────────────────────────────────────────
	{
		name: "marketing",
		terms: []kw{
			k("seo", 3), k("paid ads", 3), k("google ads", 3), k("meta ads", 3),
			k("email campaign", 3), k("social media", 3), k("copywriting", 3),
			k("brand voice", 3), k("content strateg", 3), k("landing page", 3),
			k("conversion rate", 3), k("cro", 3), k("ad creative", 3),
			k("lead generat", 3), k("app store optimization", 3),
			k("marketing plan", 3), k("programmatic seo", 3), k("growth hacking", 3),
			k("content marketing", 3), k("marketing", 2),
		},
	},

	// ── Sales ──────────────────────────────────────────────────────────────────
	{
		name: "sales",
		terms: []kw{
			k("sales pipeline", 3), k("prospect", 3), k("cold email", 3),
			k("crm", 3), k("sales outreach", 3), k("account research", 3),
			k("revenue operations", 3), k("sales engineer", 3), k("rfp", 3),
			k("battlecard", 3), k("sales forecast", 3),
		},
	},

	// ── Product Management ─────────────────────────────────────────────────────
	{
		name: "product",
		terms: []kw{
			k("sprint planning", 3), k("user story", 3), k("backlog", 3),
			k("prd", 3), k("product manager", 3), k("feature prioriti", 3),
			k("product discovery", 3), k("agile", 3), k("scrum", 3),
			k("product roadmap", 3), k("product strateg", 3), k("okr", 2),
			k("product analytic", 3), k("rice priorit", 3),
			k("user research", 3), k("kanban", 3), k("jira", 3),
		},
	},

	// ── Design & UX ───────────────────────────────────────────────────────────
	{
		name: "design",
		terms: []kw{
			k("figma", 3), k("ui/ux", 3), k("accessibility", 3), k("wcag", 3),
			k("design system", 3), k("design token", 3), k("wireframe", 3),
			k("a11y", 3), k("ux research", 3), k("design critique", 3),
			k("ui design", 3), k("visual design", 2), k("user persona", 3),
		},
	},

	// ── Data & Analytics ──────────────────────────────────────────────────────
	{
		name: "data",
		terms: []kw{
			k("sql query", 3), k("etl", 3), k("data pipeline", 3),
			k("data quality", 3), k("snowflake", 3), k("data engineer", 3),
			k("cohort analys", 3), k("a/b test", 3), k("statistical", 3),
			k("data scientist", 3), k("data model", 3), k("data viz", 3),
			k("analytics tracking", 3), k("dataset", 2), k("metrics", 2),
			k("dbt", 3), k("bigquery", 3), k("redshift", 3), k("data warehouse", 3),
		},
	},

	// ── Operations ────────────────────────────────────────────────────────────
	{
		name: "operations",
		terms: []kw{
			k("runbook", 3), k("incident commander", 3), k("on-call", 3),
			k("change management", 3), k("capacity plan", 3), k("sre", 3),
			k("postmortem", 3), k("sla", 3), k("process optimization", 3),
			k("incident response", 2), k("operational", 2), k("oncall", 3),
		},
	},

	// ── Research ──────────────────────────────────────────────────────────────
	{
		name: "research",
		terms: []kw{
			k("web research", 3), k("literature review", 3), k("research synthesis", 3),
			k("competitive research", 3), k("market research", 3),
			k("research summary", 3), k("research agent", 3), k("research report", 3),
			k("external research", 3), k("perplexity", 3), k("web search", 2),
			k("investigat", 2), k("synthesiz", 2),
		},
	},

	// ── Writing & Documentation ───────────────────────────────────────────────
	{
		name: "writing",
		terms: []kw{
			k("technical writing", 3), k("blog post", 3), k("documentation audit", 3),
			k("internal comms", 3), k("content writing", 3), k("changelog", 3),
			k("press release", 3), k("newsletter", 3), k("docx", 3),
			k("humaniz", 3), k("copy edit", 3), k("content humaniz", 3),
			k("api documentation", 3),
		},
	},

	// ── People & HR ───────────────────────────────────────────────────────────
	{
		name: "people",
		terms: []kw{
			k("hiring", 3), k("recruiting", 3), k("performance review", 3),
			k("chro", 3), k("org design", 3), k("compensation", 3),
			k("onboarding", 2), k("interview process", 3), k("culture", 2),
			k("talent", 2), k("people management", 3),
		},
	},

	// ── Agents & Orchestration ────────────────────────────────────────────────
	// Listed near the end because "agent" appears in many non-agent skills.
	// Name-boost scoring still correctly weights agent-named skills highly.
	{
		name: "agents",
		terms: []kw{
			k("multi-agent", 3), k("subagent", 3), k("parallel agent", 3),
			k("orchestrat", 2), k("agent dispatch", 3), k("agent protocol", 3),
			k("agent workflow", 3), k("dispatch plan", 3), k("spawn agent", 3),
			k("handoff package", 3), k("agent design", 3), k("agent architect", 3),
			k("agent", 1),
		},
	},

	// ── Engineering (broad — last so narrower categories win) ─────────────────
	{
		name: "engineering",
		terms: []kw{
			k("code review", 3), k("refactor", 3), k("unit test", 3),
			k("integration test", 3), k("e2e test", 3), k("tech debt", 3),
			k("api design", 3), k("performance profil", 3), k("monorepo", 3),
			k("compile", 3), k("linting", 3), k(" tdd ", 3), k("mutation test", 3),
			k("dependency audit", 3), k("migration", 2), k("database schema", 3),
			k(" git ", 2), k("debug", 3), k("architecture", 2), k("build", 2),
			k("test suite", 3), k("release", 2), k("deploy", 2), k(" api ", 2),
			k("end-to-end", 2), k("module/area", 3), k("feature repair", 3),
			k("codebase", 2), k("pull request", 3),
		},
	},
}

// ClusterCategory infers a category from a skill's name and description.
// Name tokens carry triple weight versus description tokens to ensure that
// a skill named "marketing-ops" is not hijacked by a passing mention of
// "agent" in its description.
// Returns "general" when no category clears the minScore threshold.
//
// Text is padded with spaces on both sides so that space-bounded keyword
// terms (e.g. " aws ") do not false-positive on substrings like "draws".
func ClusterCategory(name, description string) string {
	// Normalise: lower-case, replace hyphens with spaces, pad with spaces.
	nameText := " " + strings.ToLower(strings.ReplaceAll(name, "-", " ")) + " "
	descText := " " + strings.ToLower(description) + " "

	bestCat := "general"
	bestScore := 0

	for _, cat := range taxonomy {
		// Score the full combined text once …
		fullScore := score(nameText+" "+descText, cat)
		// … then add 2× the name-only score to reflect name-token dominance.
		s := fullScore + 2*score(nameText, cat)
		if s > bestScore && s >= minScore {
			bestScore = s
			bestCat = cat.name
		}
	}

	return bestCat
}

// EnrichCategories fills the Category field for any skill whose Category is
// empty or "general", using ClusterCategory.  Skills that already carry an
// explicit category from the gateway are left untouched.
func EnrichCategories(skills []client.Skill) []client.Skill {
	out := make([]client.Skill, len(skills))
	for i, s := range skills {
		if s.Category == "" || s.Category == "general" {
			s.Category = ClusterCategory(s.Name, s.Description)
		}
		out[i] = s
	}
	return out
}

// score tallies weighted keyword matches against a single text string.
func score(text string, cat category) int {
	total := 0
	for _, t := range cat.terms {
		if strings.Contains(text, t.phrase) {
			total += t.weight
		}
	}
	return total
}

// CategoryNames returns the list of all known category names in taxonomy order.
// Useful for help text and autocomplete.
func CategoryNames() []string {
	names := make([]string, len(taxonomy))
	for i, c := range taxonomy {
		names[i] = c.name
	}
	return names
}

// unexported accessor used in tests only — avoids exposing the field directly.
//
//nolint:unused // test-only accessor; kept for white-box tests in this package
func (c category) displayName() string { return c.name }
