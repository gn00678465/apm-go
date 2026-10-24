package deploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/apm-go/apm/internal/rootfs"
)

// orderedMCPFile is an MCP config file whose key order is part of the
// contract, as the Oracle writes it from Python dicts in insertion order.
type orderedMCPFile struct {
	// keyOrder names every key an entry can have, in the order written.
	keyOrder []string
	perm     os.FileMode
	// toml: codex's config.toml, written as tomlkit.dumps writes it.
	toml bool
	// noNewline: the file does not end with a newline, as json.dumps
	// leaves it.
	noNewline bool
}

var (
	claudeMCPFile = orderedMCPFile{keyOrder: claudeMCPKeyOrder, perm: mcpPermKeep}
	// adapters/client/gemini.py:106-111: json.dumps with no newline, 0600.
	antigravityMCPFile = orderedMCPFile{keyOrder: antigravityMCPKeyOrder, perm: 0600, noNewline: true}
	// adapters/client/codex.py:126: tomlkit.dumps, 0600.
	codexMCPFile = orderedMCPFile{keyOrder: codexMCPKeyOrder, perm: 0600, toml: true}
)

// mcpPermKeep, as the perm of an MCP config writer, keeps the mode of an
// existing file and gives a new one 0644 narrowed by the umask, as Python's
// Path.write_text does: the Oracle writes the project's .mcp.json so
// (adapters/client/claude.py:240).
const mcpPermKeep os.FileMode = 0

func writeMCPBytes(rw *rootfs.RootWriter, rel string, data []byte, perm os.FileMode) error {
	if perm == mcpPermKeep {
		return rw.WriteFileAtomic(rel, data)
	}
	return rw.WriteFileAtomicMode(rel, data, perm)
}

// writeOrderedMCP is writeMergedMCPJSON for a file of shape f. The document
// and the servers it already has keep their order; a server written for the
// first time goes last, in the order of names; the keys of a written server
// follow f.keyOrder, then the keys of the existing entry that no writer
// manages. An object inside an entry is an *orderedObject.
func writeOrderedMCP(projectDir, rel, topKey string, entries map[string]map[string]any, considered map[string]bool, names []string, f orderedMCPFile) error {
	rw, err := rootfs.OpenRootWriter(projectDir)
	if err != nil {
		return err
	}
	defer rw.Close()

	root := newOrderedObject()
	data, err := rw.ReadFile(rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read existing %s: %w", rw.Path(rel), err)
	case f.toml:
		if root, err = decodeOrderedTOML(data); err != nil {
			return fmt.Errorf("existing %s is not valid, refusing to overwrite: %w", rw.Path(rel), err)
		}
	default:
		parsed, perr := decodeOrderedJSON(data)
		if obj, isObject := parsed.(*orderedObject); isObject {
			root = obj
			break
		}
		// The refusal carries encoding/json's own error for the file, the
		// text this line had before the ordered decoder (ruling R13). A
		// file that holds null is an empty document, as it was then.
		var probe map[string]any
		uerr := json.Unmarshal(data, &probe)
		if uerr == nil {
			uerr = perr
		}
		if uerr != nil {
			return fmt.Errorf("existing %s is not valid, refusing to overwrite: %w", rw.Path(rel), uerr)
		}
	}

	servers, _ := root.vals[topKey].(*orderedObject)
	if servers == nil {
		servers = newOrderedObject()
	}
	build := func(name string) *orderedObject {
		entry := entries[name]
		out := newOrderedObject()
		for _, key := range f.keyOrder {
			if val, ok := entry[key]; ok {
				out.set(key, val)
			}
		}
		if prev, ok := servers.vals[name].(*orderedObject); ok {
			for _, key := range prev.keys {
				if !managedMCPKeys[key] {
					out.set(key, prev.vals[key])
				}
			}
		}
		return out
	}
	for _, name := range append([]string(nil), servers.keys...) {
		if !considered[name] {
			continue
		}
		if _, written := entries[name]; written {
			servers.set(name, build(name))
		} else {
			servers.remove(name)
		}
	}
	for _, name := range names {
		if _, written := entries[name]; !written {
			continue
		}
		if _, exists := servers.vals[name]; !exists {
			servers.set(name, build(name))
		}
	}
	root.set(topKey, servers)

	var out []byte
	if f.toml {
		out, err = encodeTOMLkit(root)
	} else {
		out, err = encodeOrderedJSON(root)
		if err == nil && !f.noNewline {
			out = append(out, '\n')
		}
	}
	if err != nil {
		return err
	}
	return writeMCPBytes(rw, rel, out, f.perm)
}
