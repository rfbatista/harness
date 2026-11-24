package vfs

import "operators-mcp/internal/ports"

// refuseProductOwner is what the product owner's shell refuses (ADR 0110): it
// writes the project's and the tasks' documents and nothing else, and it
// never deletes. A refused edit leaves nothing changed: the caller puts the
// paths back as they were stored.
func refuseProductOwner(sh *root, ed edit) ports.ShellAnswer {
	if ed.op == "rm" {
		for i, rel := range ed.rel {
			if ed.in[i] {
				return refuse("PRODUCT_OWNER_NO_DELETE", "the product owner never deletes a task or a document: "+rel+" was kept; ask the person to delete it")
			}
		}
	}
	for i, rel := range ed.rel {
		if !ed.in[i] {
			continue
		}
		a := classify(rel)
		if a.docs() || (ed.op == "mkdir" && a == layout) {
			continue
		}
		return refuse("PRODUCT_OWNER_READ_ONLY", "the product owner writes only task and project documents: "+rel+" was not written")
	}
	// A move onto a document would replace it, which deletes it.
	if ed.op == "mv" && len(ed.rel) == 2 && ed.in[1] {
		if _, exists := sh.files[ed.rel[1]]; exists {
			return refuse("PRODUCT_OWNER_NO_DELETE", "the product owner never deletes a document: moving onto "+ed.rel[1]+" would replace it")
		}
	}
	return allow
}
