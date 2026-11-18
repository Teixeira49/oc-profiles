// Package backup keeps an integrity-checked mirror of the oc-profiles store,
// outside both ~/.config/opencode and the store directory, so the store can be
// recovered even if it is deleted or corrupted.
//
// Layout of the backup directory:
//
//	current.json            newest snapshot
//	history/snap-<seq>.json last MaxHistory snapshots (rotated)
package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the snapshot document format version.
const SchemaVersion = 1

// MaxHistory is how many snapshots history/ keeps.
const MaxHistory = 10

// Version is the oc-profiles version recorded in snapshots (set by main).
var Version = "dev"

// Now is the clock used for snapshot timestamps; tests may override it.
var Now = time.Now

// Document is one snapshot file.
type Document struct {
	Schema    int             `json:"schema"`
	Seq       int64           `json:"seq"`
	CreatedAt string          `json:"createdAt"`
	Version   string          `json:"ocProfilesVersion"`
	SHA256    string          `json:"sha256"`
	Payload   json.RawMessage `json:"payload"`
}

// Payload is the self-contained store content inside a snapshot.
type Payload struct {
	Profiles  json.RawMessage `json:"profiles"`
	Templates json.RawMessage `json:"templates"`
}

// Info describes a snapshot file for listings.
type Info struct {
	Path      string
	Seq       int64
	CreatedAt string
	Profiles  int
	Valid     bool
	Err       error
	Current   bool
	SHA256    string
}

func currentPath(dir string) string { return filepath.Join(dir, "current.json") }
func historyDir(dir string) string  { return filepath.Join(dir, "history") }
func historyPath(dir string, seq int64) string {
	return filepath.Join(historyDir(dir), fmt.Sprintf("snap-%06d.json", seq))
}

// checksum hashes the compact form of the payload, so indentation of the
// snapshot file does not matter.
func checksum(payload []byte) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, payload); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// buildPayload wraps the raw store files into a payload.
func buildPayload(profilesRaw, templatesRaw []byte) ([]byte, error) {
	if !json.Valid(profilesRaw) {
		return nil, errors.New("profiles.json is not valid JSON")
	}
	if len(templatesRaw) == 0 {
		templatesRaw = []byte("null")
	}
	if !json.Valid(templatesRaw) {
		return nil, errors.New("templates.json is not valid JSON")
	}
	return marshal(Payload{Profiles: profilesRaw, Templates: templatesRaw}, false)
}

func marshal(v any, indent bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Read loads and verifies one snapshot file.
func Read(path string) (Document, Payload, Info, error) {
	info := Info{Path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		info.Err = err
		return Document{}, Payload{}, info, err
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		info.Err = fmt.Errorf("invalid snapshot: %w", err)
		return doc, Payload{}, info, info.Err
	}
	info.Seq, info.CreatedAt, info.SHA256 = doc.Seq, doc.CreatedAt, doc.SHA256
	if doc.Schema != SchemaVersion {
		info.Err = fmt.Errorf("unsupported snapshot schema %d", doc.Schema)
		return doc, Payload{}, info, info.Err
	}
	sum, err := checksum(doc.Payload)
	if err != nil || sum != doc.SHA256 {
		info.Err = errors.New("checksum mismatch (snapshot is corrupt)")
		return doc, Payload{}, info, info.Err
	}
	var p Payload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		info.Err = fmt.Errorf("invalid payload: %w", err)
		return doc, p, info, info.Err
	}
	var prof struct {
		Profiles []json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(p.Profiles, &prof); err != nil {
		info.Err = fmt.Errorf("invalid profiles in payload: %w", err)
		return doc, p, info, info.Err
	}
	info.Profiles = len(prof.Profiles)
	info.Valid = true
	return doc, p, info, nil
}

// List returns every snapshot (history plus current when it is not also in
// history), newest first, valid or not.
func List(dir string) []Info {
	var out []Info
	if !filepath.IsAbs(dir) {
		return nil
	}
	seen := map[int64]bool{}
	_, _, cur, curErr := Read(currentPath(dir))
	hasCurrent := !errors.Is(curErr, os.ErrNotExist)
	entries, _ := os.ReadDir(historyDir(dir))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "snap-") {
			continue
		}
		_, _, info, _ := Read(filepath.Join(historyDir(dir), e.Name()))
		if info.Seq == 0 {
			info.Seq = seqFromName(e.Name())
		}
		if hasCurrent && cur.Valid && info.Valid && info.Seq == cur.Seq {
			info.Current = true
		}
		seen[info.Seq] = true
		out = append(out, info)
	}
	if hasCurrent && (!cur.Valid || !seen[cur.Seq]) {
		cur.Current = true
		out = append(out, cur)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	return out
}

func seqFromName(name string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, "snap-"), ".json"), 10, 64)
	return n
}

// maxSeq returns the highest sequence number present in the mirror, valid or
// not, so sequence numbers never repeat.
func maxSeq(dir string) int64 {
	var max int64
	for _, info := range List(dir) {
		if info.Seq > max {
			max = info.Seq
		}
	}
	return max
}

// Candidates returns every valid snapshot, highest sequence first, across
// current.json and history. Invalid snapshots are reported as warnings.
func Candidates(dir string) ([]Info, []string) {
	var valid []Info
	var warnings []string
	for _, info := range List(dir) {
		if !info.Valid {
			warnings = append(warnings, fmt.Sprintf("skipped %s: %v", info.Path, info.Err))
			continue
		}
		valid = append(valid, info)
	}
	return valid, warnings
}

// NewestValid returns the valid snapshot with the highest sequence number
// across current.json and history.
func NewestValid(dir string) (Payload, Info, []string, bool) {
	valid, warnings := Candidates(dir)
	for _, info := range valid {
		if _, p, info, err := Read(info.Path); err == nil {
			return p, info, warnings, true
		}
	}
	return Payload{}, Info{}, warnings, false
}

// Find returns the valid snapshot with the given sequence number.
func Find(dir string, seq int64) (Payload, Info, error) {
	for _, info := range List(dir) {
		if info.Seq != seq {
			continue
		}
		if !info.Valid {
			return Payload{}, info, fmt.Errorf("snapshot #%d is invalid: %v", seq, info.Err)
		}
		_, p, info, err := Read(info.Path)
		return p, info, err
	}
	return Payload{}, Info{}, fmt.Errorf("snapshot #%d not found in %s", seq, dir)
}

// Write stores a new snapshot of the given store files unless current.json
// already holds an identical, valid payload. It returns the snapshot info and
// whether a new snapshot was written.
func Write(dir string, profilesRaw, templatesRaw []byte) (Info, bool, error) {
	if !filepath.IsAbs(dir) {
		return Info{}, false, fmt.Errorf("backup dir must be an absolute path, got %q", dir)
	}
	payload, err := buildPayload(profilesRaw, templatesRaw)
	if err != nil {
		return Info{}, false, err
	}
	sum, err := checksum(payload)
	if err != nil {
		return Info{}, false, err
	}
	if _, _, cur, err := Read(currentPath(dir)); err == nil && cur.SHA256 == sum {
		return cur, false, nil
	}
	doc := Document{
		Schema:    SchemaVersion,
		Seq:       maxSeq(dir) + 1,
		CreatedAt: Now().UTC().Format(time.RFC3339),
		Version:   Version,
		SHA256:    sum,
		Payload:   payload,
	}
	data, err := marshal(doc, true)
	if err != nil {
		return Info{}, false, err
	}
	if err := os.MkdirAll(historyDir(dir), 0o700); err != nil {
		return Info{}, false, err
	}
	if err := writeAtomic(historyPath(dir, doc.Seq), data); err != nil {
		return Info{}, false, err
	}
	if err := writeAtomic(currentPath(dir), data); err != nil {
		return Info{}, false, err
	}
	if err := rotate(dir); err != nil {
		return Info{}, true, fmt.Errorf("rotate history: %w", err)
	}
	_, _, info, err := Read(currentPath(dir))
	if err != nil {
		return info, true, fmt.Errorf("verify written snapshot: %w", err)
	}
	return info, true, nil
}

// rotate keeps only the MaxHistory newest snapshots in history/.
func rotate(dir string) error {
	entries, err := os.ReadDir(historyDir(dir))
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "snap-") && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return seqFromName(names[i]) > seqFromName(names[j]) })
	for _, n := range names[min(len(names), MaxHistory):] {
		if err := os.Remove(filepath.Join(historyDir(dir), n)); err != nil {
			return err
		}
	}
	return nil
}

// writeAtomic writes via temp file + fsync + rename + directory fsync.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}
