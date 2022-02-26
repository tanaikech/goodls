// Package main (goodls.go) :
// These methods are for downloading shared files from Google Drive.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/urfave/cli"
)

const (
	appname = "goodls"
	envval  = "GOODLS_APIKEY"
	anyurl  = "https://drive.google.com/uc?export=download"
	docutl  = "https://docs.google.com/"
)

// chunks : For io.Reader
type chunks struct {
	io.Reader
	cChunk int64
	Size   int64
}

// para : Structure for each parameter
type para struct {
	APIKey                string
	Client                *http.Client
	Code                  string
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
}

// Read : For io.Reader
func (c *chunks) Read(dat []byte) (int, error) {
	n, err := c.Reader.Read(dat)
	c.cChunk += int64(n)
	if err == nil {
		if c.Size > 0 {
			fmt.Printf("\rDownloading (bytes)... %d / %d", c.cChunk, c.Size)
		} else {
			fmt.Printf("\rDownloading (bytes)... %d", c.cChunk)
		}
	}
	return n, err
}

// saveFile : Save retrieved data as a file.
func (p *para) saveFile(res *http.Response) error {
	var err error
	p.ContentType = res.Header["Content-Type"][0]
	if err = p.getFilename(res); err != nil {
		return err
	}
	var file *os.File
	if p.DownloadBytes == -1 {
		file, err = os.Create(filepath.Join(p.WorkDir, p.Filename))
	} else {
		file, err = os.OpenFile(filepath.Join(p.WorkDir, p.Filename), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	}
	if err != nil {
		return err
	}
	if p.Disp {
		_, err = io.Copy(file, res.Body)
	} else {
		if p.APIKey != "" {
			_, err = io.Copy(file, &chunks{
				Reader: res.Body,
				Size:   p.Size,
			})
		} else {
			_, err = io.Copy(file, &chunks{Reader: res.Body})
		}
	}
	if err != nil {
		return err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	if !p.Disp {
		fmt.Printf("\n")
	}
	fmt.Printf("{\"Filename\": \"%s\", \"Type\": \"%s\", \"MimeType\": \"%s\", \"FileSize\": %d}\n", p.Filename, p.Kind, p.ContentType, fileInfo.Size())
	defer func() {
		file.Close()
		res.Body.Close()
	}()
	return nil
}

// getFilename : Retrieve filename from header.
func (p *para) getFilename(s *http.Response) error {
	if len(s.Header["Content-Disposition"]) > 0 {
		_, para, err := mime.ParseMediaType(s.Header["Content-Disposition"][0])
		if err != nil {
			return err
		}
		if p.Filename == "" {
			p.Filename = para["filename"]
		}
	} else {
		body, _ := ioutil.ReadAll(s.Body)
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
func (p *para) downloadLargeFile() error {
	fmt.Println("Now downloading.")
	if p.APIKey != "" {
		dlfile, err := p.getFileInfFromP()
		if err != nil {
			return err
		}
		p.Size = dlfile.Size
	}
	res, err := p.fetch(p.URL + "&confirm=" + p.Code)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 && p.Kind != "file" {
		return fmt.Errorf("error: This error occurs when it downloads a large file of Google Docs.\nMessage: %+v", res)
	}
	return p.saveFile(res)
}

// getDownloadCode : When a large size of file is downloaded, a code for downloading is retrieved at here.
func (p *para) getDownloadCode(res *http.Response) error {
	body, _ := ioutil.ReadAll(res.Body)
	rFilename := regexp.MustCompile(`confirm\=([\w\s\S]+?)"`)
	matches := rFilename.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return fmt.Errorf("file ID [ %s ] cannot be downloaded", p.ID)
	}
	p.Code = matches[0][1]
	return nil
}

// fetch : Fetch data from Google Drive
func (p *para) fetch(url string) (*http.Response, error) {
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
func (p *para) checkURL(s string) error {
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
func (p *para) download(url string) error {
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
		// After February, 2022, "Content-Disposition" is not included in the response header for a large file.
		_, chk := res.Header["Content-Disposition"]
		if chk {
			return p.saveFile(res)
		}
		if err := p.getDownloadCode(res); err != nil {
			return err
		}
		if len(p.Code) == 0 && p.Kind == "file" {
			return fmt.Errorf("file ID [ %s ] is not shared, while the file is existing", p.ID)
		} else if len(p.Code) == 0 && p.Kind != "file" {
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

// handler : Initialize of "para".
func handler(c *cli.Context) error {
	var err error
	workdir := c.String("directory")
	if workdir == "" {
		workdir, err = filepath.Abs(".")
		if err != nil {
			return err
		}
	}
	p := &para{
		APIKey:            c.String("apikey"),
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
		InputtedMimeType: func(mime string) []string {
			if mime != "" {
				return regexp.MustCompile(`\s*,\s*`).Split(mime, -1)
			}
			return nil
		}(c.String("mimetype")),
		Notcreatetopdirectory: c.Bool("notcreatetopdirectory"),
	}
	if envv := os.Getenv(envval); c.String("apikey") == "" && envv != "" {
		p.APIKey = strings.TrimSpace(envv)
	}
	if term.IsTerminal(int(syscall.Stdin)) {
		if c.String("url") == "" {
			createHelp().Run(os.Args)
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
			if scanner.Text() == "end" {
				break
			}
			urls = append(urls, scanner.Text())
		}
		if scanner.Err() != nil {
			return scanner.Err()
		}
		if len(urls) == 0 {
			return fmt.Errorf("no URL data. Please check help\n\n $ %s --help", appname)
		}
		for _, url := range urls {
			err = p.download(url)
			if err != nil {
				fmt.Printf("## Skipped: Error: %v", err)
			}
			p.Filename = ""
		}
	}
	return nil
}

// createHelp : Create help document.
func createHelp() *cli.App {
	a := cli.NewApp()
	a.Name = appname
	a.Authors = []cli.Author{
		{Name: "tanaike [ https://github.com/tanaikech/" + appname + " ] ", Email: "tanaike@hotmail.com"},
	}
	a.UsageText = "Download shared files on Google Drive."
	a.Version = "2.0.1"
	a.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "url, u",
			Usage: "URL of shared file on Google Drive. This is a required parameter.",
		},
		&cli.StringFlag{
			Name:  "extension, e",
			Usage: "Extension of output file. This is for only Google Docs (Spreadsheet, Document, Presentation).",
			Value: "pdf",
		},
		&cli.StringFlag{
			Name:  "filename, f",
			Usage: "Filename of file which is output. When this was not used, the original filename on Google Drive is used.",
		},
		&cli.StringFlag{
			Name:  "mimetype, m",
			Usage: "mimeType (You can retrieve only files with the specific mimeType, when files are downloaded from a folder.) ex. '-m \"mimeType1,mimeType2\"'",
		},
		&cli.StringFlag{
			Name:  "resumabledownload, r",
			Usage: "File is downloaded as the resumable download. For example, when '-r 1m' is used, the size of 1 MB is downloaded and create new file or append the existing file. API key is required.",
		},
		&cli.BoolFlag{
			Name:  "NoProgress, np",
			Usage: "When this option is used, the progression is not shown.",
		},
		&cli.BoolFlag{
			Name:  "overwrite, o",
			Usage: "When filename of downloading file is existing in directory at local PC, overwrite it. At default, it is not overwritten.",
		},
		&cli.BoolFlag{
			Name:  "skip, s",
			Usage: "When filename of downloading file is existing in directory at local PC, skip it. At default, it is not overwritten.",
		},
		&cli.BoolFlag{
			Name:  "fileinf, i",
			Usage: "Retrieve file information. API key is required.",
		},
		&cli.StringFlag{
			Name:  "apikey, key",
			Usage: "API key is used to retrieve file list from shared folder and file information.",
		},
		&cli.StringFlag{
			Name:  "directory, d",
			Usage: "Directory for saving downloaded files. When this is not used, the files are saved to the current working directory.",
		},
		&cli.BoolFlag{
			Name:  "notcreatetopdirectory, ntd",
			Usage: "When this option is NOT used (default situation), when a folder including subfolders is downloaded, the top folder which is downloaded is created as the top directory under the working directory. When this option is used, the top directory is not created and all files and subfolders under the top folder are downloaded under the working directory.",
		},
		&cli.BoolFlag{
			Name:  "skiperror, se",
			Usage: "When the files are downloaded from the folder, if an error occurs, the error is skipped by this option.",
		},
	}
	return a
}

// main : Main of this script
func main() {
	a := createHelp()
	a.Action = handler
	err := a.Run(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
