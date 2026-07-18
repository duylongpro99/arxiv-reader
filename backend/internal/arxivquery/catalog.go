// Package arxivquery is a dependency-free leaf package: it owns the arXiv
// category catalog and the query value object that renders an arXiv
// `search_query`. It imports nothing internal on purpose — config, tools, and
// orchestrator all depend on it, so keeping it a leaf avoids an import cycle
// (e.g. config validating its default category against the catalog).
package arxivquery

// Category is one arXiv cs.* subcategory: its arXiv code (the value used in a
// `cat:` filter) and a human-readable label for the UI dropdown.
type Category struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// Categories is the full arXiv Computer Science (cs.*) taxonomy. It is hardcoded
// rather than scraped: the list is small and stable, so a network dependency
// would add a failure mode for no real benefit (KISS/YAGNI). It serves two
// roles — the validation whitelist (see IsValid) and the UI dropdown source
// (served by GET /categories).
var Categories = []Category{
	{"cs.AI", "Artificial Intelligence"},
	{"cs.AR", "Hardware Architecture"},
	{"cs.CC", "Computational Complexity"},
	{"cs.CE", "Computational Engineering, Finance, and Science"},
	{"cs.CG", "Computational Geometry"},
	{"cs.CL", "Computation and Language"},
	{"cs.CR", "Cryptography and Security"},
	{"cs.CV", "Computer Vision and Pattern Recognition"},
	{"cs.CY", "Computers and Society"},
	{"cs.DB", "Databases"},
	{"cs.DC", "Distributed, Parallel, and Cluster Computing"},
	{"cs.DL", "Digital Libraries"},
	{"cs.DM", "Discrete Mathematics"},
	{"cs.DS", "Data Structures and Algorithms"},
	{"cs.ET", "Emerging Technologies"},
	{"cs.FL", "Formal Languages and Automata Theory"},
	{"cs.GL", "General Literature"},
	{"cs.GR", "Graphics"},
	{"cs.GT", "Computer Science and Game Theory"},
	{"cs.HC", "Human-Computer Interaction"},
	{"cs.IR", "Information Retrieval"},
	{"cs.IT", "Information Theory"},
	{"cs.LG", "Machine Learning"},
	{"cs.LO", "Logic in Computer Science"},
	{"cs.MA", "Multiagent Systems"},
	{"cs.MM", "Multimedia"},
	{"cs.MS", "Mathematical Software"},
	{"cs.NA", "Numerical Analysis"},
	{"cs.NE", "Neural and Evolutionary Computing"},
	{"cs.NI", "Networking and Internet Architecture"},
	{"cs.OH", "Other Computer Science"},
	{"cs.OS", "Operating Systems"},
	{"cs.PF", "Performance"},
	{"cs.PL", "Programming Languages"},
	{"cs.RO", "Robotics"},
	{"cs.SC", "Symbolic Computation"},
	{"cs.SD", "Sound"},
	{"cs.SE", "Software Engineering"},
	{"cs.SI", "Social and Information Networks"},
	{"cs.SY", "Systems and Control"},
}

// validCodes is an O(1) membership set built once from Categories at package
// init, so IsValid never linear-scans the slice per request.
var validCodes = func() map[string]bool {
	m := make(map[string]bool, len(Categories))
	for _, c := range Categories {
		m[c.Code] = true
	}
	return m
}()

// IsValid reports whether code is a known cs.* category. It is the single
// whitelist gate: the trigger handler rejects anything false with a 400, and
// config load rejects an unknown default. Matching is exact (case-sensitive) —
// arXiv codes are canonical, so "cs.ai" or "cat:cs.AI" are correctly rejected.
func IsValid(code string) bool {
	return validCodes[code]
}
