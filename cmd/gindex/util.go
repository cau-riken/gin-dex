package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/G-Node/gig"
	"github.com/G-Node/gin-cli/ginclient"
	"github.com/G-Node/gin-cli/web"
	"github.com/G-Node/git-module"
	"github.com/gogits/go-gogs-client"
	log "github.com/sirupsen/logrus"
	pdfcontent "github.com/unidoc/unidoc/pdf/contentstream"
	pdf "github.com/unidoc/unidoc/pdf/model"
)

func getParsedBody(r *http.Request, obj interface{}) error {
	data, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		log.Debugf("Could not read request body: %v", err)
		return err
	}
	err = json.Unmarshal(data, obj)
	if err != nil {
		log.Debugf("Could not unmarshal request [%s]: %v", string(data), err)
		return err
	}
	return nil
}

func getParsedResponse(resp *http.Response, obj interface{}) error {
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}

func getParsedHttpCall(method, path string, body io.Reader, token, csrfT string, obj interface{}) error {
	// TODO: Use client libraries
	client := &http.Client{}
	req, _ := http.NewRequest(method, path, body)
	req.Header.Set("Cookie", fmt.Sprintf("i_like_gogits=%s; _csrf=%s", token, csrfT))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %d, %s", resp.Status, resp.StatusCode, req.URL)
	}
	return getParsedResponse(resp, obj)
}

// Encodes a given map into a struct.
// Lazyly Uses json package instead of reflecting directly
func map2struct(in interface{}, out interface{}) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// Find gin repos under a certain directory, to which the authenticated user has access
func findRepos(rpath string, rbd *ReIndexRequest, gins *GinServer) ([]*gogs.Repository, error) {
	var repos []*gogs.Repository
	err := filepath.Walk(rpath, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			return nil
		}
		repo, err := gig.OpenRepository(path)
		if err != nil {
			return nil
		}
		gRepo, err := hasRepoAccess(repo, rbd, gins)
		if err != nil {
			log.Debugf("Failed to access repo: %v", err)
			return filepath.SkipDir
		}
		repos = append(repos, gRepo)
		return filepath.SkipDir
	})
	return repos, err
}

func hasRepoAccess(repository *gig.Repository, rbd *ReIndexRequest, gins *GinServer) (*gogs.Repository, error) {
	client := ginclient.New("gin")
	client.Token = rbd.Token
	gRepo, err := client.GetRepo(repository.Path)
	return &gRepo, err
}

func GetIndexCommitId(id, repoid string) gig.SHA1 {
	return sha1.Sum([]byte(repoid + id))
}

func GetIndexBlobId(id, repoid string) gig.SHA1 {
	return sha1.Sum([]byte(repoid + id))
}

func GetBlobPath(blid, cid, path string) (string, error) {
	cmd := git.NewCommand("ls-tree", "-r", cid)
	res, err := cmd.RunInDirBytes(path)
	if err != nil {
		return "", err
	}
	pattern := fmt.Sprintf("%s\\s+(.+)", blid)
	re := regexp.MustCompile(pattern)
	line_match := re.FindStringSubmatch(string(res))
	if len(line_match) > 1 {
		return line_match[1], nil
	} else {
		return "", fmt.Errorf("Not found")
	}
}

func GetPlainPdf(blobBuffer *bufio.Reader, size int64) (string, error) {
	// todo skip the creation of byte[] -> do directly
	data, err := ioutil.ReadAll(blobBuffer)
	if err != nil {
		return "", err
	}
	pdoc, err := pdf.NewPdfReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	isEncrypted, err := pdoc.IsEncrypted()
	if err != nil {
		return "", err
	}

	if isEncrypted {
		return "", fmt.Errorf("PDF encrypted")
	}

	numPages, err := pdoc.GetNumPages()
	if err != nil {
		return "", err
	}
	for i := 0; i < numPages; i++ {
		pageNum := i + 1

		page, err := pdoc.GetPage(pageNum)
		if err != nil {
			return "", err
		}

		contentStreams, err := page.GetContentStreams()
		if err != nil {
			return "", err
		}

		// If the value is an array, the effect shall be as if all of the streams in the array were concatenated,
		// in order, to form a single stream.
		pageContentStr := ""
		for _, cstream := range contentStreams {
			pageContentStr += cstream
		}
		cstreamParser := pdfcontent.NewContentStreamParser(pageContentStr)
		return cstreamParser.ExtractText()
	}
	return "", fmt.Errorf("Could not extract text from PDF")
}

func GetNevComments(blobBuf *bufio.Reader) (*string, error) {
	// get the header
	header, err := blobBuf.Peek(332)
	if err != nil {
		return nil, err
	}
	comment := string(header[76:332])
	return &comment, nil

}

func getOkRepoIds(rbd *SearchRequest, gins *GinServer) ([]string, error) {
	repos := []gogs.Repository{}
	if rbd.UserID > -10 {
		client := ginclient.New("gin")

		client.Token = rbd.Token
		res, err := client.Get("/api/v1/user/repos")
		if err != nil {
			log.Debugf("Failed to query GIN server: %v", err)
			return nil, err // return error from Get() directly
		}
		defer web.CloseRes(res.Body)
		b, err := ioutil.ReadAll(res.Body)
		if err != nil {
			log.Debugf("Failed reading response from GIN server: %v", err)
			return nil, err
		}
		err = json.Unmarshal(b, &repos)
		if err != nil {
			log.Debugf("Failed to unmarshal server response: %v", err)
		}
	}

	// Get repos ids for public repos
	prepos := struct{ Data []gogs.Repository }{}
	err := getParsedHttpCall(http.MethodGet, fmt.Sprintf("%s/api/v1/repos/search/?limit=10000", gins.URL),
		nil, rbd.Token, rbd.CsrfT, &prepos)
	if err != nil {
		log.Errorf("Could not query public repos: %v", err)
		return nil, err
	}
	repos = append(repos, prepos.Data...)

	repids := make([]string, len(repos))
	for c, repo := range repos {
		repids[c] = fmt.Sprintf("%d", repo.ID)
	}
	return repids, nil
}

func UniqueStr(in []string) []string {
	tmpM := make(map[string]struct{})
	for _, data := range in {
		tmpM[data] = struct{}{}
	}
	out := make([]string, len(tmpM))
	i := 0
	for key, _ := range tmpM {
		out[i] = key
		i++
	}
	return out
}
