package knowledgefactory

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sl6117/automations/internal/kf"
)

// publisher writes sealed files through the GitHub REST Contents API. It only ever
// creates: a path that already exists is compared and either skipped (same bytes) or
// reported as an error (different bytes), never updated.
type publisher struct {
	client *http.Client
	base   string
	token  string
	cfg    PublishConfig
	log    *log.Logger
}

// blobSHA is git's object id for a file's bytes, which is the sha the Contents API
// reports for an existing file.
func blobSHA(data []byte) string {
	h := sha1.New()
	h.Write([]byte("blob " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func (p *publisher) contentsURL(path string) string {
	segs := strings.Split(p.cfg.PathPrefix+"/"+path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return p.base + "/repos/" + p.cfg.Repo + "/contents/" + strings.Join(segs, "/")
}

func (p *publisher) do(ctx context.Context, method, u string, body []byte) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "automations-knowledge-factory")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

// existing returns the blob sha at path, or "" when the path does not exist.
func (p *publisher) existing(ctx context.Context, path string) (string, error) {
	status, body, err := p.do(ctx, http.MethodGet, p.contentsURL(path)+"?ref="+url.QueryEscape(p.cfg.Branch), nil)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	switch status {
	case http.StatusNotFound:
		return "", nil
	case http.StatusOK:
		var f struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		}
		if err := json.Unmarshal(body, &f); err != nil || f.Type != "file" || f.SHA == "" {
			return "", fmt.Errorf("read %s: unexpected response (not a single file)", path)
		}
		return f.SHA, nil
	default:
		return "", fmt.Errorf("read %s: github %d: %s", path, status, snippet(body))
	}
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

type putBody struct {
	Message string `json:"message"`
	Content string `json:"content"`
	Branch  string `json:"branch"`
}

// create puts a new file; there is deliberately no sha field, so GitHub refuses to
// replace an existing file instead of silently overwriting it.
func (p *publisher) create(ctx context.Context, path string, data []byte, message string) (int, []byte, error) {
	body, err := json.Marshal(putBody{Message: message, Content: base64.StdEncoding.EncodeToString(data), Branch: p.cfg.Branch})
	if err != nil {
		return 0, nil, err
	}
	return p.do(ctx, http.MethodPut, p.contentsURL(path), body)
}

// ensure makes path hold data, creating it if absent. Returns whether it created it.
func (p *publisher) ensure(ctx context.Context, path string, data []byte, message string) (bool, error) {
	want := blobSHA(data)
	check := func() (bool, error) {
		sha, err := p.existing(ctx, path)
		if err != nil {
			return false, err
		}
		if sha == "" {
			return false, nil
		}
		if sha != want {
			return true, fmt.Errorf("%s/%s already exists in %s with different content; a sealed file never changes, so it was left untouched", p.cfg.PathPrefix, path, p.cfg.Repo)
		}
		return true, nil
	}
	if present, err := check(); err != nil || present {
		return false, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		status, body, err := p.create(ctx, path, data, message)
		if err != nil {
			return false, fmt.Errorf("create %s: %w", path, err)
		}
		if status == http.StatusCreated || status == http.StatusOK {
			return true, nil
		}
		if status != http.StatusConflict && status != http.StatusUnprocessableEntity {
			return false, fmt.Errorf("create %s: github %d: %s", path, status, snippet(body))
		}
		// Another writer may have created it between our read and write: re-read once.
		if present, err := check(); err != nil || present {
			return false, err
		}
		if attempt == 1 {
			return false, fmt.Errorf("create %s: github %d twice: %s", path, status, snippet(body))
		}
	}
	return false, nil
}

// publishRevision publishes one sealed revision, data files first and the manifest last,
// so a manifest in the repo means the whole revision is there. A revision whose manifest
// is already published (with the same bytes) is skipped without touching its data files.
func (p *publisher) publishRevision(ctx context.Context, date string, rev int, files []kf.File) error {
	manifest := files[len(files)-1]
	present, err := p.existingMatches(ctx, manifest)
	if err != nil || present {
		return err
	}
	for _, f := range files {
		kind := strings.SplitN(f.Path, "/", 2)[0]
		created, err := p.ensure(ctx, f.Path, f.Data, fmt.Sprintf("desk-feed: %s r%d %s", date, rev, kind))
		if err != nil {
			return err
		}
		if created {
			p.log.Printf("[%s] published %s/%s", projectName, p.cfg.PathPrefix, f.Path)
		}
	}
	return nil
}

func (p *publisher) existingMatches(ctx context.Context, f kf.File) (bool, error) {
	sha, err := p.existing(ctx, f.Path)
	if err != nil || sha == "" {
		return false, err
	}
	if sha != blobSHA(f.Data) {
		return false, fmt.Errorf("%s/%s already exists in %s with different content; a sealed file never changes, so it was left untouched", p.cfg.PathPrefix, f.Path, p.cfg.Repo)
	}
	return true, nil
}
