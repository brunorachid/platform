package bitbucket

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudway/platform/config"
	"github.com/cloudway/platform/pkg/rest"
	"github.com/cloudway/platform/pkg/serverlog"
	"github.com/cloudway/platform/scm"
)

type bitbucketClient struct {
	*rest.Client
}

func init() {
	old := scm.New
	scm.New = func() (scm.SCM, error) {
		scmtype := config.Get("scm.type")
		if scmtype != "bitbucket" {
			return old()
		}

		scmurl := config.Get("scm.url")
		if scmurl == "" {
			return nil, errors.New("Bitbucket URL not configured")
		}
		u, err := url.Parse(scmurl)
		if err != nil {
			return nil, err
		}

		username := u.User.Username()
		password, _ := u.User.Password()
		auth := string(base64.StdEncoding.EncodeToString([]byte(username + ":" + password)))

		headers := map[string]string{
			"Authorization":     "Basic " + auth, // TODO
			"X-Atlassian-Token": "no-check",
			"Accept":            "application/json",
		}

		cli, err := rest.NewClient(scmurl, "", nil, headers)
		if err != nil {
			return nil, err
		}
		return &bitbucketClient{cli}, nil
	}
}

func (cli *bitbucketClient) Type() string {
	return "git"
}

func (cli *bitbucketClient) CreateNamespace(namespace string) error {
	opts := CreateProjectOpts{
		Key:  namespace,
		Name: namespace,
	}

	path := "/rest/api/1.0/projects"
	resp, err := cli.Post(context.Background(), path, nil, opts, nil)
	resp.EnsureClosed()
	return checkNamespaceError(namespace, resp, err)
}

func (cli *bitbucketClient) RemoveNamespace(namespace string) error {
	ctx := context.Background()
	start := 0
	for {
		page, err := cli.getRepoPage(ctx, namespace, start)
		if err != nil {
			return err
		}

		for _, repo := range page.Values {
			err := cli.purgeRepo(ctx, namespace, repo.Slug)
			if err != nil {
				return err
			}
		}

		start = page.NextPageStart
		if page.IsLastPage {
			break
		}
	}

	path := fmt.Sprintf("/rest/api/1.0/projects/%s", namespace)
	resp, err := cli.Delete(context.Background(), path, nil, nil)
	resp.EnsureClosed()
	return checkNamespaceError(namespace, resp, err)
}

func (cli *bitbucketClient) getRepoPage(ctx context.Context, namespace string, start int) (page *RepoPage, err error) {
	var (
		path   = fmt.Sprintf("/rest/api/1.0/projects/%s/repos", namespace)
		params = url.Values{"start": []string{strconv.Itoa(start)}}
	)
	resp, err := cli.Get(ctx, path, params, nil)
	if err == nil {
		page = new(RepoPage)
		err = json.NewDecoder(resp.Body).Decode(page)
		resp.Body.Close()
	} else {
		err = checkNamespaceError(namespace, resp, err)
	}
	return
}

func (cli *bitbucketClient) purgeRepo(ctx context.Context, namespace, name string) error {
	path := fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s", namespace, name)
	resp, err := cli.Delete(ctx, path, nil, nil)
	resp.EnsureClosed()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	} else {
		return checkServerError(resp, err)
	}
}

func (cli *bitbucketClient) CreateRepo(namespace, name string, purge bool) error {
	opts := CreateRepoOpts{
		Name: name,
	}

	var (
		ctx    = context.Background()
		path   = fmt.Sprintf("/rest/api/1.0/projects/%s/repos", namespace)
		purged = false
	)

	for {
		resp, err := cli.Post(ctx, path, nil, opts, nil)
		resp.EnsureClosed()

		// remove the garbage repository
		if purge && !purged && resp.StatusCode == http.StatusConflict {
			err = cli.purgeRepo(ctx, namespace, name)
			purged = true
			if err == nil {
				continue
			}
		}

		if err == nil {
			break
		}

		switch resp.StatusCode {
		case http.StatusNotFound:
			return scm.NamespaceNotFoundError(namespace)
		case http.StatusConflict:
			return scm.RepoExistError(name)
		default:
			return checkServerError(resp, err)
		}
	}

	// enable post-receive hook
	const hookKey = "com.cloudway.bitbucket.plugins.repo-deployer:repo-deployer"
	path = fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s/settings/hooks/%s/enabled", namespace, name, hookKey)
	resp, err := cli.Put(ctx, path, nil, nil, nil)
	resp.EnsureClosed()
	return checkServerError(resp, err)
}

func (cli *bitbucketClient) RemoveRepo(namespace, name string) error {
	path := fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s", namespace, name)
	resp, err := cli.Delete(context.Background(), path, nil, nil)
	resp.EnsureClosed()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return scm.RepoNotFoundError(name)
	default:
		return checkServerError(resp, err)
	}
}

func (cli *bitbucketClient) Populate(namespace, name string, payload io.Reader, size int64) error {
	path := fmt.Sprintf("/rest/deploy/1.0/projects/%s/repos/%s/populate", namespace, name)

	// check to see if repository already populated
	resp, err := cli.Head(context.Background(), path, nil, nil)
	resp.EnsureClosed()
	if resp.StatusCode == http.StatusForbidden {
		return nil
	} else if err != nil {
		return checkServerError(resp, err)
	}

	headers := map[string][]string{
		"Content-Type":   {"application/tar"},
		"Content-Length": {strconv.FormatInt(size, 10)},
	}
	resp, err = cli.PutRaw(context.Background(), path, nil, payload, headers)
	resp.EnsureClosed()
	return checkNamespaceError(namespace, resp, err)
}

var allowedSchemes = []string{
	"git", "http", "https", "ftp", "ftps", "rsync",
}

func isAllowedScheme(scheme string) bool {
	for _, s := range allowedSchemes {
		if s == scheme {
			return true
		}
	}
	return false
}

func (cli *bitbucketClient) PopulateURL(namespace, name, remote string) error {
	u, err := url.Parse(remote)
	if err != nil {
		return err
	}
	if u.Scheme == "" || !isAllowedScheme(u.Scheme) {
		return fmt.Errorf("Unsupported Git clone scheme: %s", u.Scheme)
	}

	path := fmt.Sprintf("/rest/deploy/1.0/projects/%s/repos/%s/populate", namespace, name)

	// check to see if repository already populated
	resp, err := cli.Head(context.Background(), path, nil, nil)
	resp.EnsureClosed()
	if resp.StatusCode == http.StatusForbidden {
		return nil
	} else if err != nil {
		return checkServerError(resp, err)
	}

	// populate repository from template URL
	query := url.Values{"url": []string{remote}}
	resp, err = cli.Post(context.Background(), path, query, nil, nil)
	resp.EnsureClosed()
	return checkNamespaceError(namespace, resp, err)
}

func (cli *bitbucketClient) Deploy(namespace, name string, branch string, log *serverlog.ServerLog) error {
	if log == nil {
		log = serverlog.Discard
	}

	path := fmt.Sprintf("/rest/deploy/1.0/projects/%s/repos/%s/deploy", namespace, name)
	query := url.Values{"branch": []string{branch}}
	resp, err := cli.Post(context.Background(), path, query, nil, nil)
	if err != nil {
		return checkNamespaceError(namespace, resp, err)
	} else {
		defer resp.Body.Close()
		return serverlog.Drain(resp.Body, log.Stdout(), log.Stderr(), nil)
	}
}

func (cli *bitbucketClient) GetDeploymentBranch(namespace, name string) (branch *scm.Branch, err error) {
	path := fmt.Sprintf("/rest/deploy/1.0/projects/%s/repos/%s/settings", namespace, name)
	resp, err := cli.Get(context.Background(), path, nil, nil)
	if err == nil {
		branch = new(scm.Branch)
		err = json.NewDecoder(resp.Body).Decode(branch)
		resp.Body.Close()
	} else {
		err = checkNamespaceError(namespace, resp, err)
	}
	return
}

func (cli *bitbucketClient) GetDeploymentBranches(namespace, name string) ([]*scm.Branch, error) {
	branches, err := cli.getRefs(namespace, name, "branches")
	if err != nil {
		return nil, err
	}

	tags, err := cli.getRefs(namespace, name, "tags")
	if err != nil {
		return nil, err
	}

	return append(branches, tags...), nil
}

func (cli *bitbucketClient) getRefs(namespace, name, typ string) (refs []*scm.Branch, err error) {
	var (
		path  = fmt.Sprintf("/rest/api/1.0/projects/%s/repos/%s/%s", namespace, name, typ)
		ctx   = context.Background()
		start = 0
	)
	for {
		page, er := cli.getRefPage(ctx, path, namespace, start)
		if er != nil {
			err = er
			break
		}
		refs = append(refs, page.Values...)
		start = page.NextPageStart
		if page.IsLastPage {
			break
		}
	}
	return
}

func (cli *bitbucketClient) getRefPage(ctx context.Context, path, namespace string, start int) (page *BranchPage, err error) {
	params := url.Values{"start": []string{strconv.Itoa(start)}}
	resp, err := cli.Get(ctx, path, params, nil)
	if err == nil {
		page = new(BranchPage)
		err = json.NewDecoder(resp.Body).Decode(page)
		resp.Body.Close()
	} else {
		err = checkNamespaceError(namespace, resp, err)
	}
	return
}

func (cli *bitbucketClient) AddKey(namespace string, key string) error {
	opts := SSHKey{}
	opts.Key.Text = key
	opts.Permission = "PROJECT_WRITE"

	path := fmt.Sprintf("/rest/keys/1.0/projects/%s/ssh", namespace)
	resp, err := cli.Post(context.Background(), path, nil, opts, nil)
	resp.EnsureClosed()

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return scm.InvalidKeyError{}
	case http.StatusConflict:
		return fmt.Errorf("SSH key already exists")
	default:
		return checkNamespaceError(namespace, resp, err)
	}
}

func (cli *bitbucketClient) RemoveKey(namespace string, key string) error {
	ctx := context.Background()
	keys, err := cli.listKeys(ctx, namespace)
	if err != nil {
		return err
	}

	for _, k := range keys {
		if strings.TrimSpace(k.Key.Text) == strings.TrimSpace(key) {
			path := fmt.Sprintf("/rest/keys/1.0/projects/%s/ssh/%d", namespace, k.Key.Id)
			resp, err := cli.Delete(ctx, path, nil, nil)
			resp.EnsureClosed()
			if err = checkServerError(resp, err); err != nil {
				return err
			}
		}
	}

	return nil
}

func (cli *bitbucketClient) ListKeys(namespace string) ([]scm.SSHKey, error) {
	keys, err := cli.listKeys(context.Background(), namespace)
	if err != nil {
		return nil, err
	}

	result := make([]scm.SSHKey, len(keys))
	for i, k := range keys {
		result[i].Label = k.Key.Label
		result[i].Text = k.Key.Text
	}
	return result, nil
}

func (cli *bitbucketClient) listKeys(ctx context.Context, namespace string) (keys []SSHKey, err error) {
	start := 0
	for {
		page, er := cli.getKeyPage(ctx, namespace, start)
		if er != nil {
			err = er
			break
		}
		keys = append(keys, page.Values...)
		start = page.NextPageStart
		if page.IsLastPage {
			break
		}
	}
	return
}

func (cli *bitbucketClient) getKeyPage(ctx context.Context, namespace string, start int) (page *SSHKeyPage, err error) {
	var (
		path   = fmt.Sprintf("/rest/keys/1.0/projects/%s/ssh", namespace)
		params = url.Values{"start": []string{strconv.Itoa(start)}}
	)
	resp, err := cli.Get(ctx, path, params, nil)
	if err == nil {
		page = new(SSHKeyPage)
		err = json.NewDecoder(resp.Body).Decode(page)
		resp.Body.Close()
	} else {
		err = checkNamespaceError(namespace, resp, err)
	}
	return
}

func checkNamespaceError(namespace string, resp *rest.ServerResponse, err error) error {
	switch resp.StatusCode {
	case http.StatusNotFound:
		return scm.NamespaceNotFoundError(namespace)
	case http.StatusConflict:
		return scm.NamespaceExistError(namespace)
	default:
		return checkServerError(resp, err)
	}
}

func checkServerError(resp *rest.ServerResponse, err error) error {
	if se, ok := err.(rest.ServerError); ok {
		var errors ServerErrors
		if json.Unmarshal(se.RawError(), &errors) == nil {
			return errors
		}
	}
	return err
}
