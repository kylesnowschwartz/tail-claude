package parser

// FilterForks examines the uuid/parentUuid DAG in a slice of entries and
// returns only the entries on the canonical (longest) path. If no fork
// points exist, the input slice is returned unchanged.
//
// Algorithm (adapted from agentsview internal/parser/claude.go):
//  1. Build uuid -> index and parentUuid -> []childIndex maps.
//  2. Find the root (entry with no parentUuid, or whose parentUuid is not
//     in the set). Fall back to linear order if there is not exactly one root.
//  3. Walk from the root. At each branch point (node with >1 child), pick
//     the child that heads the longest subtree.
//  4. Return entries in the order they appear on the chosen path.
func FilterForks(entries []Entry) []Entry {
	if len(entries) == 0 {
		return entries
	}

	// Build lookup structures.
	uuidIndex := make(map[string]int, len(entries))
	children := make(map[string][]int, len(entries)) // parentUUID -> []childIndex

	for i, e := range entries {
		if e.UUID != "" {
			uuidIndex[e.UUID] = i
		}
	}

	var roots []int
	for i, e := range entries {
		if e.ParentUUID == "" {
			roots = append(roots, i)
			continue
		}
		if _, exists := uuidIndex[e.ParentUUID]; !exists {
			// parentUuid references an entry outside this slice -- treat as root.
			roots = append(roots, i)
			continue
		}
		children[e.ParentUUID] = append(children[e.ParentUUID], i)
	}

	// Require exactly one root to proceed with DAG-aware filtering.
	// Multiple roots (disconnected graph) means we can't reliably pick a
	// canonical path, so return the original linear sequence unchanged.
	if len(roots) != 1 {
		return entries
	}

	// Check whether any fork points exist at all. If the whole graph is
	// linear we can skip the walk and return the original slice untouched.
	hasFork := false
	for _, kids := range children {
		if len(kids) > 1 {
			hasFork = true
			break
		}
	}
	if !hasFork {
		return entries
	}

	// subtreeSize returns the number of entries reachable from startIdx
	// following only the longest-child path at each fork (depth-first length).
	// We use memoization to avoid recomputing shared subtrees in diamond-shaped
	// DAGs, which would otherwise cause exponential blowup.
	memo := make(map[int]int, len(entries))
	var subtreeSize func(startIdx int) int
	subtreeSize = func(startIdx int) int {
		if v, ok := memo[startIdx]; ok {
			return v
		}
		count := 0
		current := startIdx
		visited := make(map[int]struct{})
		for current >= 0 {
			if _, seen := visited[current]; seen {
				break // cycle guard
			}
			visited[current] = struct{}{}
			count++
			uuid := entries[current].UUID
			kids := children[uuid]
			if len(kids) == 0 {
				break
			}
			if len(kids) == 1 {
				current = kids[0]
				continue
			}
			// Fork: recurse into each child and pick the biggest subtree.
			best := -1
			bestSize := -1
			for _, kid := range kids {
				s := subtreeSize(kid)
				if s > bestSize {
					bestSize = s
					best = kid
				}
			}
			current = best
		}
		memo[startIdx] = count
		return count
	}

	// Walk from root, picking the longest-subtree child at each fork.
	var path []int
	current := roots[0]
	for current >= 0 {
		path = append(path, current)
		uuid := entries[current].UUID
		kids := children[uuid]
		if len(kids) == 0 {
			break
		}
		if len(kids) == 1 {
			current = kids[0]
			continue
		}
		// Fork point: pick the child with the largest subtree.
		best := kids[0]
		bestSize := subtreeSize(kids[0])
		for _, kid := range kids[1:] {
			s := subtreeSize(kid)
			if s > bestSize {
				bestSize = s
				best = kid
			}
		}
		current = best
	}

	// Build the result in original slice order to preserve
	// relative ordering for downstream processing.
	inPath := make(map[int]struct{}, len(path))
	for _, idx := range path {
		inPath[idx] = struct{}{}
	}

	result := make([]Entry, 0, len(path))
	for i, e := range entries {
		if _, ok := inPath[i]; ok {
			result = append(result, e)
		}
	}
	return result
}
