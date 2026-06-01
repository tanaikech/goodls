package goodls

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	cli "github.com/urfave/cli/v2"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

const (
	appname = "goodls"
	envval  = "GOODLS_APIKEY"
	anyurl  = "https://drive.google.com/uc?export=download"
	docutl  = "https://docs.google.com/"
)

// Para : Structure for each parameter
type Para struct {
	APIKey                string
	Client                *http.Client
	ContentType           string
	Disp                  bool
	DlFolder              bool
	DownloadBytes         int64
	Ext                   string
	Filename              string
	ID                    string
	InputtedMimeType      []string
	Kind                  string
	Notcreatetopdirectory bool
	OverWrite             bool
	Resumabledownload     string
	SearchID              string
	ShowFileInf           bool
	Size                  int64
	Skip                  bool
	SkipError             bool
	URL                   string
	WorkDir               string
	URLForLargeFile       string
	Concurrency           int

	ConflictStrategy string
	ConflictResolved bool
	MCPMode          bool // True when operating inside an MCP server

	Progress    *mpb.Progress
	ResultJSONs *[]string
	mu          *sync.Mutex
}

// Clone : Deep copy necessary fields to prevent race conditions during concurrent execution.
func (p *Para) Clone() *Para {
	newP := *p
	return &newP
}

// resolveConflict handles file existing conflicts
func (p *Para) resolveConflict(targetPath string, remoteTime time.Time) (string, string, error) {
	if !chkFile(targetPath) {
		return targetPath, "overwrite", nil
	}

	strategy := p.ConflictStrategy

	if strategy == "prompt" && p.MCPMode {
		return "", "", fmt.Errorf("File '%s' already exists. Ask the user for the preferred conflict resolution strategy (skip, overwrite, newer, rename) and call this tool again with the explicit 'conflict' parameter.", filepath.Base(targetPath))
	}

	// Graceful fallback for non-interactive environments outside MCP
	if strategy == "prompt" && !term.IsTerminal(int(syscall.Stdin)) {
		strategy = "rename"
	}

prompt_loop:
	for {
		switch strategy {
		case "skip":
			return targetPath, "skip", nil
		case "overwrite":
			return targetPath, "overwrite", nil
		case "newer":
			localInfo, err := os.Stat(targetPath)
			if err != nil {
				return targetPath, "overwrite", nil
			}
			if remoteTime.IsZero() {
				if !p.Disp {
					p.mu.Lock()
					fmt.Fprintf(os.Stderr, "[*] Warning: Cannot determine remote time for '%s'. Falling back to 'rename'.\n", filepath.Base(targetPath))
					p.mu.Unlock()
				}
				strategy = "rename"
				continue prompt_loop
			}
			if remoteTime.After(localInfo.ModTime()) {
				return targetPath, "overwrite", nil
			}
			return targetPath, "skip", nil
		case "rename":
			ext := filepath.Ext(targetPath)
			base := targetPath[:len(targetPath)-len(ext)]

			timestamp := time.Now().Format("20060102_150405")
			newPath := fmt.Sprintf("%s_%s%s", base, timestamp, ext)
			if !chkFile(newPath) {
				return newPath, "overwrite", nil
			}

			for i := 1; ; i++ {
				newPath = fmt.Sprintf("%s_%s_%d%s", base, timestamp, i, ext)
				if !chkFile(newPath) {
					return newPath, "overwrite", nil
				}
			}
		case "prompt":
			p.mu.Lock()
			fmt.Fprintf(os.Stderr, "\r\033[K[Conflict] File '%s' already exists.\n", filepath.Base(targetPath))
			if p.DlFolder {
				fmt.Fprintf(os.Stderr, "Hint: You are downloading a folder. Use '--conflict [skip|overwrite|newer|rename]' to avoid repeated prompts.\n")
			}
			fmt.Fprintf(os.Stderr, "Choose action - [s]kip, [o]verwrite, [n]ewer, [r]ename, [a]bort: ")

			var input string
			var b [1]byte
			// Raw read to explicitly prevent bufio from consuming the pipe stream ahead of time
			for {
				n, err := os.Stdin.Read(b[:])
				if err != nil || n == 0 {
					break
				}
				if b[0] == '\n' {
					break
				}
				if b[0] != '\r' {
					input += string(b[0])
				}
			}
			p.mu.Unlock()

			input = strings.TrimSpace(strings.ToLower(input))
			switch input {
			case "s", "skip":
				return targetPath, "skip", nil
			case "o", "overwrite":
				return targetPath, "overwrite", nil
			case "n", "newer":
				strategy = "newer"
			case "r", "rename":
				strategy = "rename"
			case "a", "abort":
				return targetPath, "abort", fmt.Errorf("download aborted by user for %s", targetPath)
			default:
				p.mu.Lock()
				fmt.Fprintf(os.Stderr, "Invalid input. Please try again.\n")
				p.mu.Unlock()
			}
		default:
			return targetPath, "skip", fmt.Errorf("unknown conflict strategy: %s", strategy)
		}
	}
}

// getURLFromHTML : Get the download URL from HTML.
func (p *Para) getURLFromHTML(html *http.Response) error {
	br := html.Body
	doc, err := goquery.NewDocumentFromReader(br)
	if err != nil {
		return err
	}
	form := doc.Find("form[id='download-form']")
	url, b := form.Attr("action")
	if !b {
		return fmt.Errorf("specification of the endpoint for downloading the file might have been changed")
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	form.Find("input").Each(func(i int, s *goquery.Selection) {
		t, b := s.Attr("type")
		if t == "hidden" && b {
			name, _ := s.Attr("name")
			value, _ := s.Attr("value")
			q.Add(name, value)
		}
	})
	req.URL.RawQuery = q.Encode()
	p.URLForLargeFile = req.URL.String()
	return nil
}

// saveFile : Save retrieved data as a file with progress bar integration.
func (p *Para) saveFile(res *http.Response) error {
	defer res.Body.Close()

	var err error
	if len(res.Header["Content-Type"]) > 0 {
		p.ContentType = res.Header["Content-Type"][0]
	}
	if err = p.getFilename(res); err != nil {
		return err
	}

	targetPath := filepath.Join(p.WorkDir, p.Filename)

	if p.DownloadBytes == -1 && !p.ConflictResolved {
		var remoteTime time.Time
		if lastModified := res.Header.Get("Last-Modified"); lastModified != "" {
			if t, err := http.ParseTime(lastModified); err == nil {
				remoteTime = t
			}
		}

		resolvedPath, action, err := p.resolveConflict(targetPath, remoteTime)
		if err != nil {
			return err
		}
		if action == "skip" {
			p.Filename = filepath.Base(targetPath)
			if !p.Disp {
				p.mu.Lock()
				fmt.Fprintf(os.Stderr, "[*] Skipped: '%s' already exists.\n", p.Filename)
				p.mu.Unlock()
			}
			return nil
		}

		p.Filename = filepath.Base(resolvedPath)
		targetPath = resolvedPath
		p.ConflictResolved = true
	}

	var file *os.File
	if p.DownloadBytes == -1 {
		file, err = os.Create(targetPath)
	} else {
		file, err = os.OpenFile(targetPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	}
	if err != nil {
		return err
	}
	defer file.Close()

	var reader io.Reader = res.Body

	if p.Size <= 0 {
		p.Size = res.ContentLength
	}

	var bar *mpb.Bar
	if !p.Disp && p.Progress != nil && !p.MCPMode {
		nameDecor := decor.Name(p.Filename, decor.WCSyncSpaceR)
		if p.Size > 0 {
			bar = p.Progress.AddBar(p.Size,
				mpb.PrependDecorators(
					nameDecor,
					decor.CountersKibiByte("% .2f / % .2f", decor.WCSyncSpaceR),
				),
				mpb.AppendDecorators(
					decor.EwmaETA(decor.ET_STYLE_GO, 90),
					decor.Name(" ] "),
					decor.Percentage(),
				),
			)
		} else {
			bar = p.Progress.AddBar(0,
				mpb.PrependDecorators(
					nameDecor,
					decor.CurrentKibiByte("% .2f", decor.WCSyncSpaceR),
				),
				mpb.AppendDecorators(
					decor.OnComplete(decor.Spinner(nil), "Done"),
				),
			)
		}
		proxy := bar.ProxyReader(res.Body)
		reader = proxy
	}

	_, err = io.Copy(file, reader)

	if bar != nil {
		if err != nil {
			bar.Abort(false)
		} else {
			bar.SetTotal(-1, true)
		}
	}

	if err != nil {
		return err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}

	resJSON := fmt.Sprintf("{\"Filename\": \"%s\", \"Type\": \"%s\", \"MimeType\": \"%s\", \"FileSize\": %d}", p.Filename, p.Kind, p.ContentType, fileInfo.Size())
	p.mu.Lock()
	*p.ResultJSONs = append(*p.ResultJSONs, resJSON)
	if p.Disp && !p.MCPMode {
		fmt.Println(resJSON)
	}
	p.mu.Unlock()

	return nil
}

// getFilename : Retrieve filename from header.
func (p *Para) getFilename(s *http.Response) error {
	if len(s.Header["Content-Disposition"]) > 0 {
		_, paraMap, err := mime.ParseMediaType(s.Header["Content-Disposition"][0])
		if err != nil {
			return err
		}
		if p.Filename == "" {
			p.Filename = paraMap["filename"]
		}
	} else {
		body, _ := io.ReadAll(s.Body)
		rFilename := regexp.MustCompile(`<span class="uc-name-size"><a[\w\s\S]+?>([\w\s\S]+?)<\/a>`)
		matches := rFilename.FindAllStringSubmatch(string(body), -1)
		if len(matches) == 0 {
			return fmt.Errorf("file ID [ %s ] cannot be downloaded", p.ID)
		}
		p.Filename = matches[0][1]
	}
	return nil
}

// downloadLargeFile : When a large size of file is downloaded, this method is used.
func (p *Para) downloadLargeFile() error {
	if p.APIKey != "" {
		dlfile, err := p.getFileInfFromP()
		if err != nil {
			return err
		}
		p.Size = dlfile.Size
	}
	res, err := p.fetch(p.URLForLargeFile)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 && p.Kind != "file" {
		return fmt.Errorf("error: This error occurs when it downloads a large file of Google Docs.\nMessage: %+v", res)
	}
	return p.saveFile(res)
}

// fetch : Fetch data from Google Drive
func (p *Para) fetch(url string) (*http.Response, error) {
	req, err := http.NewRequest("get", url, nil)
	if err != nil {
		return nil, err
	}
	res, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// checkURL : Parse inputted URL.
func (p *Para) checkURL(s string) error {
	var err error
	r := regexp.MustCompile(`google\.com\/(\w.+)\/d\/(\w.+)\/`)
	r2 := regexp.MustCompile(`drive.google.com\/uc\?(export\=\w+|id\=([\w\S]+))&(export\=\w+|id\=([\w\S]+))`)
	if r.MatchString(s) {
		res := r.FindAllStringSubmatch(s, -1)
		p.Kind = res[0][1]
		p.ID = res[0][2]
		if p.Kind == "file" {
			p.URL = anyurl + "&id=" + p.ID
		} else {
			if p.Ext == "" {
				p.Ext = "pdf"
			} else if p.Ext == "ms" {
				switch p.Kind {
				case "spreadsheets":
					p.Ext = "xlsx"
				case "document":
					p.Ext = "docx"
				case "presentation":
					p.Ext = "pptx"
				}
			}
			if p.Kind == "presentation" {
				p.URL = docutl + p.Kind + "/d/" + p.ID + "/export/" + p.Ext
			} else {
				p.URL = docutl + p.Kind + "/d/" + p.ID + "/export?format=" + p.Ext
			}
		}

		if p.APIKey != "" && p.Kind == "file" {
			p.URL = "https://www.googleapis.com/drive/v3/files/" + p.ID + "?alt=media&supportsAllDrives=true&key=" + p.APIKey
			dlfile, err := p.getFileInfFromP()
			if err != nil {
				return err
			}
			p.Filename = dlfile.Name
			p.Size = dlfile.Size
		}

		if p.APIKey != "" && p.ShowFileInf {
			if err := p.showFileInf(); err != nil {
				return err
			}
			return nil
		}
	} else if r2.MatchString(s) {
		u, err := url.Parse(s)
		if err != nil {
			return err
		}
		q := u.Query()
		p.Kind = "file"
		p.ID = q["id"][0]
		p.URL = anyurl + "&id=" + p.ID
		if p.APIKey != "" && p.ShowFileInf {
			if err := p.showFileInf(); err != nil {
				return err
			}
			return nil
		}
	} else {
		folder := regexp.MustCompile(`google\.com\/drive\/folders\/([a-zA-Z0-9-_]+)`)
		if folder.MatchString(s) {
			p.DlFolder = true
			res := folder.FindAllStringSubmatch(s, -1)
			p.SearchID = res[0][1]
			if p.APIKey != "" {
				err = p.getFilesFromFolder()
				if err != nil {
					return err
				}
			} else {
				return errors.New("please use API key to download files in a folder")
			}
		} else {
			return errors.New("URL is wrong")
		}
	}
	return nil
}

// download : Main method of download.
func (p *Para) download(url string) error {
	var err error
	err = p.checkURL(url)
	if err != nil {
		return err
	}
	if p.APIKey != "" && p.ShowFileInf {
		return nil
	} else if p.APIKey == "" && p.ShowFileInf {
		return errors.New("when you want to use the option '--fileinf', please use API key")
	} else if p.APIKey != "" && p.DlFolder {
		return nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	p.Client = &http.Client{Jar: jar}
	res, err := p.fetch(p.URL)
	if err != nil {
		return err
	}
	if res.StatusCode == 200 {
		_, chk := res.Header["Content-Disposition"]
		if chk {
			return p.saveFile(res)
		}
		if err := p.getURLFromHTML(res); err != nil {
			return err
		}
		if len(p.URLForLargeFile) == 0 && p.Kind == "file" {
			return fmt.Errorf("file ID [ %s ] is not shared, while the file is existing", p.ID)
		} else if len(p.URLForLargeFile) == 0 && p.Kind != "file" {
			return p.saveFile(res)
		} else {
			if p.APIKey != "" && p.Resumabledownload != "" {
				p.DownloadBytes, err = getDownloadBytes(p.Resumabledownload)
				if err != nil {
					return err
				}
				return p.resumableDownload()
			}
			return p.downloadLargeFile()
		}
	}
	return fmt.Errorf("file ID [ %s ] cannot be downloaded as [ %s ]", p.ID, p.Ext)
}

// handler : Initialize of "Para".
func handler(c *cli.Context) error {
	var err error
	workdir := c.String("directory")
	if workdir == "" {
		workdir, err = filepath.Abs(".")
		if err != nil {
			return err
		}
	}

	concurrency := c.Int("concurrency")
	if concurrency <= 0 {
		concurrency = 5
	}

	conflict := strings.ToLower(c.String("conflict"))
	if c.Bool("overwrite") {
		conflict = "overwrite"
	} else if c.Bool("skip") {
		conflict = "skip"
	}

	switch conflict {
	case "prompt", "skip", "overwrite", "newer", "rename":
		// valid
	default:
		return fmt.Errorf("invalid conflict strategy: %s", conflict)
	}

	p := &Para{
		Disp:              c.Bool("NoProgress"),
		DownloadBytes:     -1,
		Ext:               c.String("extension"),
		OverWrite:         c.Bool("overwrite"),
		Resumabledownload: c.String("resumabledownload"),
		ShowFileInf:       c.Bool("fileinf"),
		Skip:              c.Bool("skip"),
		SkipError:         c.Bool("skiperror"),
		WorkDir:           workdir,
		DlFolder:          false,
		Concurrency:       concurrency,
		ConflictStrategy:  conflict,
		InputtedMimeType: func(mime string) []string {
			if mime != "" {
				return regexp.MustCompile(`\s*,\s*`).Split(mime, -1)
			}
			return nil
		}(c.String("mimetype")),
		Notcreatetopdirectory: c.Bool("notcreatetopdirectory"),
		ResultJSONs:           &[]string{},
		mu:                    &sync.Mutex{},
	}

	ignoreAPIKey := c.Bool("no-apikey")
	rawKey := c.String("apikey")
	envv := os.Getenv(envval)
	var apiKeySource string

	if ignoreAPIKey {
		p.APIKey = ""
		fmt.Fprintf(os.Stderr, "[*] API Key Explicitly Ignored via --no-apikey flag. Running in anonymous access mode.\n")
	} else {
		if rawKey != "" {
			p.APIKey = strings.TrimSpace(rawKey)
			apiKeySource = "CLI Flag (--apikey / --key)"
		} else if envv != "" && strings.TrimSpace(envv) != "" {
			p.APIKey = strings.TrimSpace(envv)
			apiKeySource = fmt.Sprintf("Environment Variable (%s)", envval)
		}

		if p.APIKey != "" {
			maskedKey := p.APIKey
			if len(maskedKey) > 8 {
				maskedKey = maskedKey[:4] + strings.Repeat("*", len(maskedKey)-8) + maskedKey[len(maskedKey)-4:]
			} else {
				maskedKey = strings.Repeat("*", len(maskedKey))
			}
			fmt.Fprintf(os.Stderr, "[*] API Key Detected\n")
			fmt.Fprintf(os.Stderr, "    - Source: %s\n", apiKeySource)
			fmt.Fprintf(os.Stderr, "    - Key   : %s\n", maskedKey)
		} else {
			fmt.Fprintf(os.Stderr, "[*] No API Key provided. Running in anonymous access mode.\n")
		}
	}

	if !p.Disp {
		p.Progress = mpb.New(mpb.WithWidth(60))
	}

	if term.IsTerminal(int(syscall.Stdin)) {
		if c.String("url") == "" {
			cli.ShowAppHelp(c)
			return nil
		}
		p.Filename = c.String("filename")
		err = p.download(c.String("url"))
		if err != nil {
			return err
		}
	} else {
		var urls []string
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := scanner.Text()
			if text == "end" {
				break
			}
			urls = append(urls, text)
		}
		if scanner.Err() != nil {
			return scanner.Err()
		}
		if len(urls) == 0 {
			return fmt.Errorf("no URL data. Please check help\n\n $ %s --help", appname)
		}

		sem := make(chan struct{}, p.Concurrency)
		eg, _ := errgroup.WithContext(context.Background())
		for _, u := range urls {
			u := u
			eg.Go(func() error {
				sem <- struct{}{}
				defer func() { <-sem }()

				workerP := p.Clone()
				workerP.Filename = ""
				err := workerP.download(u)
				if err != nil {
					fmt.Fprintf(os.Stderr, "## Skipped: Error: %v\n", err)
				}
				return nil
			})
		}
		eg.Wait()
	}

	if p.Progress != nil {
		p.Progress.Wait()
		if !p.Disp {
			for _, jsonRes := range *p.ResultJSONs {
				fmt.Println(jsonRes)
			}
		}
	}

	return nil
}

// createHelp : Create help document using cli v2.
func createHelp() *cli.App {
	app := &cli.App{
		Name:    appname,
		Authors: []*cli.Author{{Name: "tanaike [ https://github.com/tanaikech/" + appname + " ] ", Email: "tanaike@hotmail.com"}},
		Usage:   "Download shared files on Google Drive.",
		Version: "3.3.1",
		Commands: []*cli.Command{
			{
				Name:  "mcp",
				Usage: "Run the tool as an MCP (Model Context Protocol) server over stdio",
				Action: func(c *cli.Context) error {
					return RunMCP()
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "url",
				Aliases: []string{"u"},
				Usage:   "URL of shared file on Google Drive. This is a required parameter.",
			},
			&cli.StringFlag{
				Name:    "extension",
				Aliases: []string{"e"},
				Usage:   "Extension of output file. This is for only Google Docs (Spreadsheet, Document, Presentation).",
				Value:   "pdf",
			},
			&cli.StringFlag{
				Name:    "filename",
				Aliases: []string{"f"},
				Usage:   "Filename of file which is output. When this was not used, the original filename on Google Drive is used.",
			},
			&cli.StringFlag{
				Name:    "mimetype",
				Aliases: []string{"m"},
				Usage:   "mimeType (You can retrieve only files with the specific mimeType, when files are downloaded from a folder.) ex. '-m \"mimeType1,mimeType2\"'",
			},
			&cli.StringFlag{
				Name:    "conflict",
				Aliases: []string{"cf"},
				Usage:   "Conflict resolution strategy when a file already exists: 'prompt', 'skip', 'overwrite', 'newer', 'rename'. Defaults to 'prompt' in terminal.",
				Value:   "prompt",
			},
			&cli.StringFlag{
				Name:    "resumabledownload",
				Aliases: []string{"r"},
				Usage:   "File is downloaded as the resumable download. For example, when '-r 1m' is used, the size of 1 MB is downloaded and create new file or append the existing file. API key is required.",
			},
			&cli.BoolFlag{
				Name:    "NoProgress",
				Aliases: []string{"np"},
				Usage:   "When this option is used, the progression is not shown.",
			},
			&cli.BoolFlag{
				Name:    "overwrite",
				Aliases: []string{"o"},
				Usage:   "Legacy flag. Overwrite existing files (same as --conflict overwrite).",
			},
			&cli.BoolFlag{
				Name:    "skip",
				Aliases: []string{"s"},
				Usage:   "Legacy flag. Skip existing files (same as --conflict skip).",
			},
			&cli.BoolFlag{
				Name:    "fileinf",
				Aliases: []string{"i"},
				Usage:   "Retrieve file information. API key is required.",
			},
			&cli.StringFlag{
				Name:    "apikey",
				Aliases: []string{"key"},
				Usage:   "API key is used to retrieve file list from shared folder and file information.",
			},
			&cli.BoolFlag{
				Name:    "no-apikey",
				Aliases: []string{"nk"},
				Usage:   "Explicitly ignore the API key even if it is set via environment variable or flag. Forces anonymous access mode.",
			},
			&cli.StringFlag{
				Name:    "directory",
				Aliases: []string{"d"},
				Usage:   "Directory for saving downloaded files. When this is not used, the files are saved to the current working directory.",
			},
			&cli.BoolFlag{
				Name:    "notcreatetopdirectory",
				Aliases: []string{"ntd"},
				Usage:   "When this option is NOT used (default situation), when a folder including subfolders is downloaded, the top folder which is downloaded is created as the top directory under the working directory.",
			},
			&cli.BoolFlag{
				Name:    "skiperror",
				Aliases: []string{"se"},
				Usage:   "When the files are downloaded from the folder, if an error occurs, the error is skipped by this option.",
			},
			&cli.IntFlag{
				Name:    "concurrency",
				Aliases: []string{"c"},
				Usage:   "Number of concurrent downloads when fetching multiple files (e.g. from a folder or stdin).",
				Value:   5,
			},
		},
		Action: handler,
	}
	return app
}

// Run : Main execution proxy for CLI commands
func Run(args []string) {
	app := createHelp()
	if err := app.Run(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
