package gitops

import (
	"fmt"
	"slices"

	"github.com/go-git/go-git/v5/plumbing/object"
)

const (
	// renameSimilarity is how alike two files must be to be one renamed,
	// as git's default: half their lines in common.
	renameSimilarity = 0.5
	// mostRenamePairs bounds the comparisons, so a merge of thousands of
	// moved files does not compare each with each.
	mostRenamePairs = 10000
	largestCompared = 1 << 20
)

// mergeUnit is one file to merge: its version in the base, ours and theirs,
// which may sit at different paths when a side renamed it, the path the
// result goes to, and the paths of ours it replaces.
type mergeUnit struct {
	base, ours, theirs *object.File
	target             string
	gone               []string
	note               string
}

// mergeUnits pairs each file's versions, following renames on either side,
// then takes every other path as it is.
func mergeUnits(files map[string][3]*object.File) []mergeUnit {
	toOurs, toTheirs := renames(files, 1), renames(files, 2)
	handled := map[string]bool{}
	var units []mergeUnit
	for _, old := range sortedKeys(toOurs, toTheirs) {
		oursPath, oursRenamed := toOurs[old]
		theirsPath, theirsRenamed := toTheirs[old]
		unit := mergeUnit{base: files[old][0], ours: files[old][1], theirs: files[old][2], target: old}
		if oursRenamed {
			unit.ours, unit.target = files[oursPath][1], oursPath
		}
		if theirsRenamed {
			unit.theirs = files[theirsPath][2]
		}
		switch {
		case oursRenamed && theirsRenamed && oursPath != theirsPath:
			unit.note = fmt.Sprintf("%s: renamed to %s here and to %s there; the content is merged into %s", old, oursPath, theirsPath, oursPath)
		case theirsRenamed && !oursRenamed:
			unit.target = theirsPath
			if files[old][1] != nil {
				unit.gone = []string{old}
			}
			if files[theirsPath][1] != nil {
				unit.note = fmt.Sprintf("%s: renamed to %s there, where a file already is here; the content is merged into it", old, theirsPath)
			}
		}
		handled[old], handled[oursPath], handled[theirsPath] = true, true, true
		units = append(units, unit)
	}
	for _, path := range sortedKeys(files) {
		if !handled[path] {
			sides := files[path]
			units = append(units, mergeUnit{base: sides[0], ours: sides[1], theirs: sides[2], target: path})
		}
	}
	return units
}

// renames finds which files of the base a side moved: gone from their path
// there and the same, or alike enough, at a new one. Exact matches go
// first, then the most alike.
func renames(files map[string][3]*object.File, side int) map[string]string {
	var removed, added []string
	for path, versions := range files {
		switch {
		case versions[0] != nil && versions[side] == nil:
			removed = append(removed, path)
		case versions[0] == nil && versions[side] != nil:
			added = append(added, path)
		}
	}
	slices.Sort(removed)
	slices.Sort(added)
	found := map[string]string{}
	taken := map[string]bool{}
	for _, old := range removed {
		for _, path := range added {
			if !taken[path] && files[old][0].Hash == files[path][side].Hash {
				found[old], taken[path] = path, true
				break
			}
		}
	}
	if len(removed)*len(added) > mostRenamePairs {
		return found
	}
	for _, old := range removed {
		if _, matched := found[old]; matched {
			continue
		}
		best, bestScore := "", renameSimilarity
		for _, path := range added {
			if taken[path] {
				continue
			}
			if score := similarity(files[old][0], files[path][side]); score >= bestScore {
				best, bestScore = path, score
			}
		}
		if best != "" {
			found[old], taken[best] = best, true
		}
	}
	return found
}

// similarity is the share of lines two files have in common, from 0 to 1.
func similarity(a, b *object.File) float64 {
	if a.Size > largestCompared || b.Size > largestCompared || isBinary(a) || isBinary(b) {
		return 0
	}
	before, after := lines(fileText(a)), lines(fileText(b))
	if len(before)+len(after) == 0 {
		return 1
	}
	common := 0
	for _, match := range matches(before, after) {
		if match >= 0 {
			common++
		}
	}
	return 2 * float64(common) / float64(len(before)+len(after))
}

func sortedKeys[V any](maps ...map[string]V) []string {
	seen := map[string]bool{}
	var keys []string
	for _, each := range maps {
		for key := range each {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	return keys
}
