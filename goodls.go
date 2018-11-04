// Package main (goodls.go) :
// These methods are for downloading shared files from Google Drive.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/urfave/cli"
)

const (
	appname = "goodls"
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
	APIKey      string
	Client      *http.Client
	Code        string
	ContentType string
	Disp        bool
	DlFolder    bool
	Ext         string
	Filename    string
	ID          string
	Kind        string
	OverWrite   bool
	SearchID    string
	ShowFileInf bool
	Size        int64
	Skip        bool
	URL         string
	WorkDir     string
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
	err = p.getFilename(res)
	if err = p.getFilename(res); err != nil {
		return err
	}
	file, err := os.Create(filepath.Join(p.WorkDir, p.Filename))
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
		return fmt.Errorf("file ID [ %s ] is not shared, while the file is existing", p.ID)
	}
	return nil
}

// downloadLargeFile : When a large size of file is downloaded, this method is used.
func (p *para) downloadLargeFile() error {
	fmt.Println("Now downloading.")
	res, err := p.fetch(p.URL + "&confirm=" + p.Code)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 && p.Kind != "file" {
		return fmt.Errorf("error: This error occurs when it downloads a large file of Google Docs.\nMessage: %+v", res)
	}
	p.saveFile(res)
	return nil
}

// checkCookie : When a large size of file is downloaded, a code for downloading is retrieved at here.
func (p *para) checkCookie(rawCookies string) {
	header := http.Header{}
	header.Add("Cookie", rawCookies)
	request := http.Request{Header: header}
	for _, e := range request.Cookies() {
		if strings.Contains(e.Name, "download_warning_") {
			cookie, _ := request.Cookie(e.Name)
			p.Code = cookie.Value
			break
		}
	}
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
			p.SearchID = p.ID
			err = p.getFilesFromFolder()
			if err != nil {
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
		return errors.New("When you want to use the option '--fileinf', please use API key")
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
		if len(res.Header["Set-Cookie"]) == 0 {
			return p.saveFile(res)
		}
		p.checkCookie(res.Header["Set-Cookie"][0])
		if len(p.Code) == 0 && p.Kind == "file" {
			return fmt.Errorf("file ID [ %s ] is not shared, while the file is existing", p.ID)
		} else if len(p.Code) == 0 && p.Kind != "file" {
			return p.saveFile(res)
		} else {
			return p.downloadLargeFile()
		}
	} else {
		return fmt.Errorf("file ID [ %s ] cannot be downloaded as [ %s ]", p.ID, p.Ext)
	}
	return nil
}

// handler : Initialize of "para".
func handler(c *cli.Context) {
	var err error
	workdir, err := filepath.Abs(".")
	if err != nil {
		log.Fatal(err)
	}
	p := &para{
		APIKey:      c.String("apikey"),
		Disp:        c.Bool("NoProgress"),
		Ext:         c.String("extension"),
		OverWrite:   c.Bool("overwrite"),
		ShowFileInf: c.Bool("fileinf"),
		Skip:        c.Bool("skip"),
		WorkDir:     workdir,
		DlFolder:    false,
	}
	if terminal.IsTerminal(int(syscall.Stdin)) {
		if c.String("url") == "" {
			createHelp().Run(os.Args)
			return
		}
		p.Filename = c.String("filename")
		err = p.download(c.String("url"))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
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
			fmt.Fprintf(os.Stderr, "Error: %v\n", scanner.Err())
			os.Exit(1)
		}
		if len(urls) == 0 {
			fmt.Fprintf(os.Stderr, "Error: No URL data. Please check help.\n\n $ %s --help\n\n", appname)
			os.Exit(1)
		}
		for _, url := range urls {
			err = p.download(url)
			if err != nil {
				fmt.Printf("## Skipped: Error: %v", err)
			}
			p.Filename = ""
		}
	}
	return
}

// createHelp : Create help document.
func createHelp() *cli.App {
	a := cli.NewApp()
	a.Name = appname
	a.Author = "tanaike [ https://github.com/tanaikech/" + appname + " ] "
	a.Email = "tanaike@hotmail.com"
	a.Usage = "Download shared files on Google Drive."
	a.Version = "1.1.0"
	a.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "url, u",
			Usage: "URL of shared file on Google Drive. This is a required parameter.",
		},
		cli.StringFlag{
			Name:  "extension, e",
			Usage: "Extension of output file. This is for only Google Docs (Spreadsheet, Document, Presentation).",
			Value: "pdf",
		},
		cli.StringFlag{
			Name:  "filename, f",
			Usage: "Filename of file which is output. When this was not used, the original filename on Google Drive is used.",
		},
		cli.BoolFlag{
			Name:  "NoProgress, np",
			Usage: "When this option is used, the progression is not shown.",
		},
		cli.BoolFlag{
			Name:  "overwrite, o",
			Usage: "When filename of downloading file is existing in directory at local PC, overwrite it. At default, it is not overwritten.",
		},
		cli.BoolFlag{
			Name:  "skip, s",
			Usage: "When filename of downloading file is existing in directory at local PC, skip it. At default, it is not overwritten.",
		},
		cli.BoolFlag{
			Name:  "fileinf, i",
			Usage: "Retrieve file information. API key is required.",
		},
		cli.StringFlag{
			Name:  "apikey, key",
			Usage: "API key is uded to retrieve file list from shared folder and file information.",
		},
	}
	return a
}

// main : Main of this script
func main() {
	a := createHelp()
	a.Action = handler
	a.Run(os.Args)
}
