package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tbal999/flowtocsv"
)

func multipartBody(t *testing.T, fields map[string]string, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fw, err := mw.CreateFormFile("files", n)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(fw, files[n])
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func do(t *testing.T, h http.Handler, method, url string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, url, body)
	req.Host = "127.0.0.1:8765"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func learnViaGUI(t *testing.T, h http.Handler) flowtocsv.Instructions {
	t.Helper()
	body, ct := multipartBody(t, map[string]string{"delimiter": "|"}, map[string]string{"D0150.txt": sample})
	rec := do(t, h, "POST", "/api/learn", body, ct)
	if rec.Code != 200 {
		t.Fatalf("learn: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Instructions flowtocsv.Instructions `json:"instructions"`
		Samples      map[string]string      `json:"samples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Instructions.Flow != "D0150" || res.Samples["g290_1"] != "m1" || res.Samples["hdr_2"] != "D0150001" {
		t.Fatalf("learn result: flow %q samples %v", res.Instructions.Flow, res.Samples)
	}
	return res.Instructions
}

func TestGUILearnSaveListGet(t *testing.T) {
	dir := t.TempDir()
	h := newGUI(dir)
	if rec := do(t, h, "GET", "/", nil, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>flowtocsv</title>") {
		t.Fatalf("index: %d", rec.Code)
	}

	in := learnViaGUI(t, h)
	in.Groups[0].Fields[0] = "mpan_core"
	js, _ := json.Marshal(in)
	if rec := do(t, h, "PUT", "/api/instructions/D0150", bytes.NewReader(js), "application/json"); rec.Code != 200 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "D0150.json")); err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, "GET", "/api/instructions", nil, "")
	if !strings.Contains(rec.Body.String(), `"name":"D0150"`) {
		t.Errorf("list = %s", rec.Body)
	}
	rec = do(t, h, "GET", "/api/instructions/D0150", nil, "")
	var got flowtocsv.Instructions
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Groups[0].Fields[0] != "mpan_core" {
		t.Errorf("get returned %+v", got.Groups[0])
	}
}

func TestGUIRejectsBadSaves(t *testing.T) {
	h := newGUI(t.TempDir())
	if rec := do(t, h, "PUT", "/api/instructions/..%2Fevil", strings.NewReader("{}"), "application/json"); rec.Code == 200 {
		t.Error("path traversal name accepted")
	}
	if rec := do(t, h, "PUT", "/api/instructions/x", strings.NewReader(`{"flow":"x"}`), "application/json"); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid instructions: %d", rec.Code)
	}
}

func TestGUILocalOnly(t *testing.T) {
	h := newGUI(t.TempDir())
	req := httptest.NewRequest("GET", "/api/instructions", nil)
	req.Host = "evil.example:8765"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign Host: %d", rec.Code)
	}

	req = httptest.NewRequest("PUT", "/api/instructions/x", strings.NewReader("{}"))
	req.Host = "127.0.0.1:8765"
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin PUT: %d", rec.Code)
	}
}

func TestGUIConvertAndExport(t *testing.T) {
	h := newGUI(t.TempDir())
	in := learnViaGUI(t, h)
	js, _ := json.Marshal(in)
	files := map[string]string{
		"a.txt":   sample,
		"bad.txt": strings.Replace(sample, "08A|x|", "999|?|", 1),
		"gas.txt": strings.Replace(sample, "D0150001", "D0010002", 1),
	}

	body, ct := multipartBody(t, map[string]string{"instructions": string(js), "layout": "tables"}, files)
	rec := do(t, h, "POST", "/api/convert", body, ct)
	if rec.Code != 200 {
		t.Fatalf("convert: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Files  []fileStatus `json:"files"`
		Tables []struct {
			Name      string `json:"name"`
			TotalRows int    `json:"total_rows"`
		} `json:"tables"`
	}
	json.Unmarshal(rec.Body.Bytes(), &res)
	status := map[string]string{}
	for _, f := range res.Files {
		status[f.Name] = f.Status
	}
	if status["a.txt"] != "converted" || status["bad.txt"] != "failed" || status["gas.txt"] != "skipped" {
		t.Errorf("statuses = %v", status)
	}
	if len(res.Tables) != 5 || res.Tables[3].Name != "d0150_293" || res.Tables[3].TotalRows != 3 {
		t.Errorf("tables = %+v", res.Tables)
	}

	body, ct = multipartBody(t, map[string]string{"instructions": string(js), "layout": "wide", "format": "csv"}, files)
	rec = do(t, h, "POST", "/api/export", body, ct)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d %s", rec.Code, rec.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "d0150.csv,_not_converted.txt" {
		t.Errorf("zip contains %v", names)
	}
}

func TestArchiveRejectedAndLog(t *testing.T) {
	dir := t.TempDir()
	instr := setup(t, dir)
	inbox := filepath.Join(dir, "inbox")
	writeT(t, filepath.Join(inbox, "a.txt"), sample)
	writeT(t, filepath.Join(inbox, "a-copy.txt"), sample)
	writeT(t, filepath.Join(inbox, "bad.txt"), strings.Replace(sample, "08A|x|", "999|?|", 1))
	writeT(t, filepath.Join(inbox, "unknown.txt"), strings.Replace(sample, "D0150001", "D0010002", 1))
	archive, rejected, logf := filepath.Join(dir, "archive"), filepath.Join(dir, "rejected"), filepath.Join(dir, "run.log")
	// A file of the same name already archived must not be overwritten.
	writeT(t, filepath.Join(archive, "a.txt"), "older")

	code, _, stderr := runT(t, "convert", "-i", instr, "-o", filepath.Join(dir, "out"),
		"-archive", archive, "-rejected", rejected, "-log", logf, inbox)
	if code != exitPartial {
		t.Fatalf("exit %d, want %d: %s", code, exitPartial, stderr)
	}

	left, _ := os.ReadDir(inbox)
	if len(left) != 1 || left[0].Name() != "unknown.txt" {
		t.Errorf("inbox should keep only the unmatched file, has %v", left)
	}
	for _, p := range []string{
		filepath.Join(archive, "a.1.txt"), filepath.Join(archive, "a-copy.txt"), filepath.Join(rejected, "bad.txt"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(archive, "a.txt")); string(b) != "older" {
		t.Error("existing archived file was overwritten")
	}
	lb, _ := os.ReadFile(logf)
	log := string(lb)
	for _, want := range []string{"run started:", `group "999"`, "moved ", "run finished: some files were rejected"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}
