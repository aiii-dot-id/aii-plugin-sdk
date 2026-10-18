package aiiospkg

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeRuntimeFiles(t *testing.T, dir string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// THE PACKER HOLDS A TREE TO WHAT THE HOST'S READER COUNTS. The reader
// admits the files, a quarter as many directories again, the root and the
// inventory. The packer counted files alone: two files in two directories
// packed under a files budget of 2, and the host refused the archive's six
// members at extraction. The same sum is held at pack time, and the refusal
// names the least budget that admits the tree.
func TestWriteRuntimeTreeCountsEveryMemberTheHostCounts(t *testing.T) {
	src := t.TempDir()
	writeRuntimeFiles(t, src, "a/one.py", "b/two.py")
	for _, files := range []int{2, 3} {
		_, err := WriteRuntimeTreeWithin(&bytes.Buffer{}, src, "cp1", TreeLimits{MaxFiles: files})
		if err == nil || !strings.Contains(err.Error(), "-max-files") || !strings.Contains(err.Error(), "the least that admits this tree is 4") {
			t.Fatalf("a files budget of %d: want a refusal naming -max-files and the least budget, 4; got %v", files, err)
		}
	}
	rep, err := WriteRuntimeTreeWithin(&bytes.Buffer{}, src, "cp1", TreeLimits{MaxFiles: 4})
	if err != nil {
		t.Fatalf("a files budget of 4 admits two files in two directories: %v", err)
	}
	if rep.Files != 2 || rep.Directories != 2 {
		t.Fatalf("the report counts %d files and %d directories, want 2 and 2", rep.Files, rep.Directories)
	}

	// The report asks the operator for the ceiling the host's reader needs,
	// not for the count of files.
	many := RuntimeArchive{Files: 30000, Directories: 12000}
	if got := many.RequiredCeilings()[CeilingFiles]; got != 33600 {
		t.Fatalf("30000 files in 12000 directories need %s >= 33600; the report asks %d", CeilingFiles, got)
	}
	few := RuntimeArchive{Files: 30000, Directories: 5000}
	if _, asked := few.RequiredCeilings()[CeilingFiles]; asked {
		t.Fatalf("30000 files in 5000 directories fit the default; the report asks %v", few.RequiredCeilings())
	}
}

// The least files ceiling is the least: it admits the tree, and one fewer
// does not.
func TestLeastFilesCeilingIsTheLeast(t *testing.T) {
	admits := func(ceiling, files, dirs int) bool { return ceiling >= files && files+dirs <= memberAllowance(ceiling) }
	for files := 0; files <= 60; files++ {
		for dirs := 0; dirs <= 60; dirs++ {
			m := leastFilesCeiling(files, dirs)
			if !admits(m, files, dirs) {
				t.Fatalf("%d files, %d directories: a ceiling of %d does not admit them", files, dirs, m)
			}
			if m > 0 && admits(m-1, files, dirs) {
				t.Fatalf("%d files, %d directories: %d is not the least, %d admits them", files, dirs, m, m-1)
			}
		}
	}
}

// NAMES THAT DIFFER ONLY IN LETTER CASE ARE REFUSED AT PACK TIME. The
// host's reader refuses two siblings whose names are equal under ASCII
// casefold and not equal; the packer wrote them, and the archive was
// refused where it was installed. Both are named.
func TestWriteRuntimeTreeRefusesNamesThatDifferOnlyInCase(t *testing.T) {
	for name, pair := range map[string][2]string{
		"two files":              {"pkg/Module.py", "pkg/module.py"},
		"two directories":        {"Lib/a.py", "lib/b.py"},
		"a file and a directory": {"pkg/Data", "pkg/data/x.py"},
	} {
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			// Where the filesystem itself folds case the two names are one
			// name, and no such tree can be made to pack.
			probe := filepath.Join(t.TempDir(), "CaseProbe")
			if err := os.WriteFile(probe, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(probe), "caseprobe")); err == nil {
				t.Skip("this filesystem folds case: two names that differ only in case cannot both be made here")
			}
			writeRuntimeFiles(t, src, pair[0], pair[1])
			_, err := WriteRuntimeTree(&bytes.Buffer{}, src, "cp1")
			if err == nil || !strings.Contains(err.Error(), "differ only in letter case") {
				t.Fatalf("want a refusal of names that differ only in letter case, got %v", err)
			}
		})
	}
	// The same name in two directories, and names that differ in more than
	// case, are not collisions.
	src := t.TempDir()
	writeRuntimeFiles(t, src, "a/module.py", "b/module.py", "a/Module2.py", "A2/module.py")
	if _, err := WriteRuntimeTree(&bytes.Buffer{}, src, "cp1"); err != nil {
		t.Fatalf("a tree with no collision is refused: %v", err)
	}
}

// The packer's one pass agrees with the rule the package writer walks pair
// by pair (casefoldSiblingCollision, the host reader's): on every set of
// paths here, it finds a collision exactly when some pair collides.
func TestCasefoldSiblingsAgreesWithThePairwiseRule(t *testing.T) {
	for _, paths := range [][]string{
		{"a", "a/x", "a/y"},
		{"a", "A"},
		{"a/x", "a/X"},
		{"a", "a/x", "A", "A/x"},
		{"pkg", "pkg/Data", "pkg/data", "pkg/data/x.py"},
		{"a/b/c", "a/B/c"},
		{"a/b/c", "a/b/C", "a/b/d"},
		{"ab", "aB", "Ab"},
		{"a/module.py", "b/module.py", "b/Module2.py"},
		{"x.PY", "x.py2", "y/x.py"},
	} {
		// casefoldSiblings is given every directory and file of a tree: each
		// path here brings its ancestors, as the packer's list does.
		seen := map[string]bool{}
		for _, p := range paths {
			seen[p] = true
			for i := 0; i < len(p); i++ {
				if p[i] == '/' {
					seen[p[:i]] = true
				}
			}
		}
		paths = paths[:0]
		for p := range seen {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		pairwise := false
		for i := range paths {
			for j := i + 1; j < len(paths); j++ {
				if casefoldSiblingCollision(paths[i], paths[j]) {
					pairwise = true
				}
			}
		}
		if _, _, got := casefoldSiblings(paths); got != pairwise {
			t.Errorf("%v: one pass says %v, the pairwise rule says %v", paths, got, pairwise)
		}
	}
}

// THE ARCHIVE'S OWN MEMBER IS HELD AMONG THE TREE'S TOP-LEVEL NAMES. The
// writer puts inventory.json beside them, and the host's reader refuses a
// second member of its path, a member below it, and a member that differs
// from it in letter case only. The packer refused a file of exactly that
// name and held nothing else, so each tree here was packed and then refused
// where it was installed. None needs a
// filesystem that keeps case: the inventory is not on disk.
func TestWriteRuntimeTreeHoldsItsInventoryAmongTheTreesNames(t *testing.T) {
	for name, tc := range map[string]struct{ rel, want string }{
		"a file of its name":                   {"inventory.json", "the tree must not carry one"},
		"a directory of its name":              {"inventory.json/child.py", "must not carry a directory of that name"},
		"a file that differs in case":          {"Inventory.json", "Inventory.json differs only in letter case from inventory.json"},
		"a directory that differs in case":     {"INVENTORY.JSON/child.py", "INVENTORY.JSON differs only in letter case from inventory.json"},
		"a deeper path under such a directory": {"Inventory.Json/a/b/child.py", "Inventory.Json differs only in letter case from inventory.json"},
	} {
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			writeRuntimeFiles(t, src, "lib/a.py", tc.rel)
			_, err := WriteRuntimeTree(&bytes.Buffer{}, src, "cp1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a refusal that says %q, got %v", tc.want, err)
			}
		})
	}
	// Below the top level the inventory has no sibling, and the name is
	// free there in any letter case; and a top-level name that only begins
	// like it is another name.
	src := t.TempDir()
	writeRuntimeFiles(t, src, "lib/inventory.json", "data/Inventory.json", "share/inventory.json/x.py", "inventory.jsonl", "Inventory")
	if _, err := WriteRuntimeTree(&bytes.Buffer{}, src, "cp1"); err != nil {
		t.Fatalf("a tree that does not collide with the inventory is refused: %v", err)
	}
}
