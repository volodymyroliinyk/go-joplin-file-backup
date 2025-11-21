package main

import (
    "bytes"
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "log"
    "mime/multipart"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "syscall"
    "time"
)

type Client struct {
    BaseURL string
    Token   string
    HTTP    *http.Client
}

type Note struct {
    ID    string `json:"id"`
    Title string `json:"title"`
    Body  string `json:"body,omitempty"`
}

type NotesResponse struct {
    Items   []Note `json:"items"`
    HasMore bool   `json:"has_more"`
}

type Resource struct {
    ID    string `json:"id"`
    Title string `json:"title"`
}

const (
    JOPLIN_API_BASE = "http://localhost:41184"
    // JOPLIN_TOKEN    = "ac41d362cc994227eec2b01c2a4f1b3a925eb20d742202f3480e516e68a916dcef7717225ba1e452a37600a48fd7fdb2c2e50b84f0659b2047ad2050cd91d289"
)

func NewClient(baseURL, token string) *Client {
    return &Client{
        BaseURL: strings.TrimRight(baseURL, "/"),
        Token:   token,
        HTTP:    &http.Client{Timeout: 15 * time.Second},
    }
}

// buildURL adds path and query parameters, including token.
func (c *Client) buildURL(path string, params map[string]string) string {
    if !strings.HasPrefix(path, "/") {
        path = "/" + path
    }
    u, err := url.Parse(c.BaseURL)
    if err != nil {
        // fallback, shouldn't normally happen
        return c.BaseURL + path
    }
    u.Path = strings.TrimRight(u.Path, "/") + path

    q := u.Query()
    if c.Token != "" {
        q.Set("token", c.Token)
    }
    for k, v := range params {
        q.Set(k, v)
    }
    u.RawQuery = q.Encode()
    return u.String()
}

func (c *Client) Ping() error {
    u := c.buildURL("/ping", nil)
    resp, err := c.HTTP.Get(u)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("ping failed: status=%d body=%s", resp.StatusCode, string(body))
    }
    return nil
}

// NotesByTitle returns all notes in the notebook (folder) as a map[title]Note.
func (c *Client) NotesByTitle(notebookId string) (map[string]Note, error) {
    result := make(map[string]Note)
    page := 1

    for {
        params := map[string]string{
            "page":   strconv.Itoa(page),
            "fields": "id,title,body",
        }
        u := c.buildURL("/folders/"+notebookId+"/notes", params)

        resp, err := c.HTTP.Get(u)
        if err != nil {
            return nil, fmt.Errorf("fetch notes page %d: %w", page, err)
        }

        var payload NotesResponse
        if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
            resp.Body.Close()
            return nil, fmt.Errorf("decode notes page %d: %w", page, err)
        }
        resp.Body.Close()

        for _, n := range payload.Items {
            result[n.Title] = n
        }

        if !payload.HasMore {
            break
        }
        page++
    }

    return result, nil
}

// UploadResource uploads a file as a Joplin resource and returns its metadata.
func (c *Client) UploadResource(path, title string) (*Resource, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, fmt.Errorf("open file: %w", err)
    }
    defer f.Close()

    var buf bytes.Buffer
    writer := multipart.NewWriter(&buf)

    fileField, err := writer.CreateFormFile("data", filepath.Base(path))
    if err != nil {
        return nil, fmt.Errorf("create form file: %w", err)
    }

    if _, err := io.Copy(fileField, f); err != nil {
        return nil, fmt.Errorf("copy file data: %w", err)
    }

    props := map[string]string{"title": title}
    propsJSON, err := json.Marshal(props)
    if err != nil {
        return nil, fmt.Errorf("marshal props: %w", err)
    }

    if err := writer.WriteField("props", string(propsJSON)); err != nil {
        return nil, fmt.Errorf("write props field: %w", err)
    }

    if err := writer.Close(); err != nil {
        return nil, fmt.Errorf("close multipart writer: %w", err)
    }

    u := c.buildURL("/resources", nil)
    req, err := http.NewRequest(http.MethodPost, u, &buf)
    if err != nil {
        return nil, fmt.Errorf("new request: %w", err)
    }
    req.Header.Set("Content-Type", writer.FormDataContentType())

    resp, err := c.HTTP.Do(req)
    if err != nil {
        return nil, fmt.Errorf("do request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 300 {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("upload resource failed: status=%d body=%s", resp.StatusCode, string(body))
    }

    var res Resource
    if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
        return nil, fmt.Errorf("decode resource: %w", err)
    }

    return &res, nil
}

// DeleteResource deletes a resource from Joplin by ID.
// Does not touch notes, notebooks, tags - only the resource file itself.
func (c *Client) DeleteResource(id string) error {
    u := c.buildURL("/resources/"+id, nil)

    req, err := http.NewRequest(http.MethodDelete, u, nil)
    if err != nil {
        return fmt.Errorf("new DELETE request: %w", err)
    }

    resp, err := c.HTTP.Do(req)
    if err != nil {
        return fmt.Errorf("do DELETE: %w", err)
    }
    defer resp.Body.Close()

    // If the resource has already been deleted, it is not critical.
    if resp.StatusCode == http.StatusNotFound {
        return nil
    }

    if resp.StatusCode >= 300 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("delete resource failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
    }

    return nil
}

// CreateNote creates a new note in the given notebook.
func (c *Client) CreateNote(notebookId, title, body string) (*Note, error) {
    payload := map[string]string{
        "title":     title,
        "parent_id": notebookId,
        "body":      body,
    }
    data, err := json.Marshal(payload)
    if err != nil {
        return nil, fmt.Errorf("marshal note: %w", err)
    }

    u := c.buildURL("/notes", nil)
    resp, err := c.HTTP.Post(u, "application/json", bytes.NewReader(data))
    if err != nil {
        return nil, fmt.Errorf("post note: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 300 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("create note failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
    }

    var note Note
    if err := json.NewDecoder(resp.Body).Decode(&note); err != nil {
        return nil, fmt.Errorf("decode note: %w", err)
    }

    return &note, nil
}

// UpdateNote updates an existing note (title, parent_id, body).
func (c *Client) UpdateNote(id, notebookId, title, body string) error {
    payload := map[string]string{
        "title":     title,
        "parent_id": notebookId,
        "body":      body,
    }
    data, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshal note update: %w", err)
    }

    u := c.buildURL("/notes/"+id, nil)
    req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(data))
    if err != nil {
        return fmt.Errorf("new PUT request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.HTTP.Do(req)
    if err != nil {
        return fmt.Errorf("do PUT: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 300 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("update note failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
    }

    return nil
}

// extractResourceIDs searches for all resource IDs from markdown links of the form [name](:/RESOURCE_ID)
func extractResourceIDs(body string) []string {
    var ids []string
    start := 0

    for {
        i := strings.Index(body[start:], "](:/")
        if i == -1 {
            break
        }
        i += start

        // resource starts after "](:/"
        idStart := i + len("](:/")
        j := strings.Index(body[idStart:], ")")
        if j == -1 {
            break
        }

        id := body[idStart : idStart+j]
        if id != "" {
            ids = append(ids, id)
        }

        start = idStart + j
    }

    return ids
}

// fileCreatedAt returns the "earliest" timestamp available for the file:
// min(modTime, atime, ctime) on Unix; on other OS falls back to ModTime().
func fileCreatedAt(info os.FileInfo) time.Time {
    t := info.ModTime()

    if stat, ok := info.Sys().(*syscall.Stat_t); ok {
        // Unix-like systems: use atime, mtime, ctime and take the earliest.
        atime := time.Unix(int64(stat.Atim.Sec), int64(stat.Atim.Nsec))
        mtime := time.Unix(int64(stat.Mtim.Sec), int64(stat.Mtim.Nsec))
        ctime := time.Unix(int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec))

        t = mtime
        if atime.Before(t) {
            t = atime
        }
        if ctime.Before(t) {
            t = ctime
        }
    }

    return t
}

func main() {
    log.SetFlags(0)

    var notebookId string
    var directory string
    var fileExtension string

    flag.StringVar(&notebookId, "notebook_id", "", "Joplin notebook (folder) ID")
    flag.StringVar(&directory, "directory", "", "Directory to scan for files")
    flag.StringVar(&fileExtension, "file_extension", ".smmx", "File extension filter (e.g. .smmx)")

    flag.Parse()

    token := os.Getenv("JOPLIN_TOKEN")
    if token == "" {
        log.Fatal("ERROR: Environment variable JOPLIN_TOKEN is not set or empty.")
    }

    dirInfo, err := os.Stat(directory)
    if err != nil {
        log.Fatalf("cannot stat directory %q: %v", directory, err)
    }
    if !dirInfo.IsDir() {
        log.Fatalf("%q is not a directory", directory)
    }

    client := NewClient(JOPLIN_API_BASE, token)

    if err := client.Ping(); err != nil {
        log.Printf("WARNING: Joplin /ping failed: %v (continuing anyway)", err)
    }

    notesByTitle, err := client.NotesByTitle(notebookId)
    if err != nil {
        log.Fatalf("failed to load notes from notebook %s: %v", notebookId, err)
    }

    fmt.Printf("Existing notes in notebook %s: %d\n", notebookId, len(notesByTitle))

    lowerExt := strings.ToLower(fileExtension)

    err = filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            log.Printf("walk error on %s: %v", path, err)
            return nil
        }
        if info.IsDir() {
            return nil
        }
        if strings.ToLower(filepath.Ext(info.Name())) != lowerExt {
            return nil
        }

        createdAt := fileCreatedAt(info)
        createdAtUTC := createdAt.UTC()
        title := info.Name()

        // Save the old resource ID for this note (if it exists)
        var oldResourceIDs []string
        var noteID string
        if note, ok := notesByTitle[title]; ok {
            noteID = note.ID
            oldResourceIDs = extractResourceIDs(note.Body)
        }

        // Loading a new resource
        res, err := client.UploadResource(path, title)
        if err != nil {
            log.Printf("ERROR uploading resource for %s: %v", path, err)
            return nil
        }

        createdAtStr := createdAt.Format("2006-01-02 15:04:05.000 -0700")
        uploadAt := time.Now()
        uploadAtStr := uploadAt.Format("2006-01-02 15:04:05.000 -0700")

        body := fmt.Sprintf(
            "created_at: %q\n"+
                "upload_at: %q\n"+
                "file_path: %q\n\n"+
                "[%s](:/%s)\n",
            createdAtStr,
            uploadAtStr,
            path,
            title,
            res.ID,
        )

        status := "added"
        if noteID != "" {
            // Update an existing note
            if err := client.UpdateNote(noteID, notebookId, title, body); err != nil {
                log.Printf("ERROR updating note for %s: %v", path, err)
            } else {
                status = "updated"

                // After successful update - delete old resources
                for _, rid := range oldResourceIDs {
                    if rid == res.ID {
                        continue
                    }
                    if err := client.DeleteResource(rid); err != nil {
                        log.Printf("WARNING: failed to delete old resource %s for %s: %v", rid, path, err)
                    } else {
                        fmt.Printf("  cleaned old resource %s for %s\n", rid, path)
                    }
                }
            }
        } else {
            // Create a new note
            note, err := client.CreateNote(notebookId, title, body)
            if err != nil {
                log.Printf("ERROR creating note for %s: %v", path, err)
            } else {
                notesByTitle[title] = *note
            }
        }

        fmt.Printf(
            "%s | created_at_utc=%s | status=%s\n",
            path,
            createdAtUTC.Format(time.RFC3339Nano),
            status,
        )

        return nil
    })

    if err != nil {
        log.Fatalf("scan error: %v", err)
    }
}
