package client

import (
	"context"
	"encoding/json"
	"io"
	"net/url"

	"github.com/cloudway/platform/api/types"
	"github.com/cloudway/platform/pkg/serverlog"
)

func (api *APIClient) GetApplications(ctx context.Context) ([]string, error) {
	var apps []string
	resp, err := api.cli.Get(ctx, "/applications/", nil, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&apps)
		resp.EnsureClosed()
	}
	return apps, err
}

func (api *APIClient) GetApplicationInfo(ctx context.Context, name string) (*types.ApplicationInfo, error) {
	var info types.ApplicationInfo
	resp, err := api.cli.Get(ctx, "/applications/"+name, nil, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&info)
		resp.EnsureClosed()
	}
	return &info, err
}

func (api *APIClient) CreateApplication(ctx context.Context, opts types.CreateApplication, dstout, dsterr io.Writer) (*types.ApplicationInfo, error) {
	resp, err := api.cli.Post(ctx, "/applications/", nil, &opts, nil)
	if err != nil {
		return nil, err
	}

	var info types.ApplicationInfo
	err = serverlog.Drain(resp.Body, dstout, dsterr, &info)
	resp.Body.Close()
	return &info, err
}

func (api *APIClient) RemoveApplication(ctx context.Context, name string) error {
	resp, err := api.cli.Delete(ctx, "/applications/"+name, nil, nil)
	resp.EnsureClosed()
	return err
}

func (api *APIClient) CreateService(ctx context.Context, dstout, dsterr io.Writer, app string, tags ...string) error {
	resp, err := api.cli.Post(ctx, "/applications/"+app+"/services/", nil, tags, nil)
	if err != nil {
		return err
	}

	err = serverlog.Drain(resp.Body, dstout, dsterr, nil)
	resp.Body.Close()
	return err
}

func (api *APIClient) RemoveService(ctx context.Context, app, service string) error {
	resp, err := api.cli.Delete(ctx, "/applications/"+app+"/services/"+service, nil, nil)
	resp.EnsureClosed()
	return err
}

func (api *APIClient) StartApplication(ctx context.Context, name string, dstout, dsterr io.Writer) error {
	resp, err := api.cli.Post(ctx, "/applications/"+name+"/start", nil, nil, nil)
	if err != nil {
		return err
	}

	err = serverlog.Drain(resp.Body, dstout, dsterr, nil)
	resp.Body.Close()
	return err
}

func (api *APIClient) StopApplication(ctx context.Context, name string) error {
	resp, err := api.cli.Post(ctx, "/applications/"+name+"/stop", nil, nil, nil)
	resp.EnsureClosed()
	return err
}

func (api *APIClient) RestartApplication(ctx context.Context, name string, dstout, dsterr io.Writer) error {
	resp, err := api.cli.Post(ctx, "/applications/"+name+"/restart", nil, nil, nil)
	if err != nil {
		return err
	}

	err = serverlog.Drain(resp.Body, dstout, dsterr, nil)
	resp.Body.Close()
	return err
}

func (api *APIClient) GetApplicationStatus(ctx context.Context, name string) (status []*types.ContainerStatus, err error) {
	resp, err := api.cli.Get(ctx, "/applications/"+name+"/status", nil, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&status)
		resp.EnsureClosed()
	}
	return
}

func (api *APIClient) GetAllApplicationStatus(ctx context.Context) (status map[string][]*types.ContainerStatus, err error) {
	resp, err := api.cli.Get(ctx, "/applications/status/", nil, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&status)
		resp.EnsureClosed()
	}
	return
}

func (api *APIClient) GetApplicationProcesses(ctx context.Context, name string) (procs []*types.ProcessList, err error) {
	resp, err := api.cli.Get(ctx, "/applications/"+name+"/procs", nil, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&procs)
		resp.EnsureClosed()
	}
	return
}

func (api *APIClient) GetApplicationStats(ctx context.Context, name string) (io.ReadCloser, error) {
	resp, err := api.cli.Get(ctx, "/applications/"+name+"/stats", nil, nil)
	return resp.Body, err
}

func (api *APIClient) DeployApplication(ctx context.Context, name, branch string, dstout, dsterr io.Writer) error {
	var query url.Values
	if branch != "" {
		query = url.Values{"branch": []string{branch}}
	}

	resp, err := api.cli.Post(ctx, "/applications/"+name+"/deploy", query, nil, nil)
	if err != nil {
		return err
	}

	err = serverlog.Drain(resp.Body, dstout, dsterr, nil)
	resp.Body.Close()
	return err
}

func (api *APIClient) GetApplicationDeployments(ctx context.Context, name string) (*types.Deployments, error) {
	var deployments types.Deployments
	resp, err := api.cli.Get(ctx, "/applications/"+name+"/deploy", nil, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&deployments)
		resp.EnsureClosed()
	}
	return &deployments, err
}

func (api *APIClient) Download(ctx context.Context, name string) (io.ReadCloser, error) {
	headers := map[string][]string{"Accept": {"application/tar+gzip"}}
	resp, err := api.cli.Get(ctx, "/applications/"+name+"/repo", nil, headers)
	return resp.Body, err
}

func (api *APIClient) Upload(ctx context.Context, name string, content io.Reader, binary bool, dstout, dsterr io.Writer) error {
	var query url.Values
	if binary {
		query = url.Values{}
		query.Set("binary", "true")
	}

	headers := map[string][]string{"Content-Type": {"application/tar+gzip"}}
	resp, err := api.cli.PutRaw(ctx, "/applications/"+name+"/repo", query, content, headers)
	if err != nil {
		return err
	}

	err = serverlog.Drain(resp.Body, dstout, dsterr, nil)
	resp.Body.Close()
	return err
}

func (api *APIClient) Dump(ctx context.Context, name string) (io.ReadCloser, error) {
	headers := map[string][]string{"Accept": {"application/tar+gzip"}}
	resp, err := api.cli.Get(ctx, "/applications/"+name+"/data", nil, headers)
	return resp.Body, err
}

func (api *APIClient) Restore(ctx context.Context, name string, content io.Reader) error {
	headers := map[string][]string{"Content-Type": {"application/tar+gzip"}}
	resp, err := api.cli.PutRaw(ctx, "/applications/"+name+"/data", nil, content, headers)
	resp.EnsureClosed()
	return err
}

func (api *APIClient) ScaleApplication(ctx context.Context, name, scaling string, dstout, dsterr io.Writer) error {
	query := url.Values{"scale": []string{scaling}}
	resp, err := api.cli.Post(ctx, "/applications/"+name+"/scale", query, nil, nil)
	if err != nil {
		return err
	}

	serverlog.Drain(resp.Body, dstout, dsterr, nil)
	resp.Body.Close()
	return err
}

func envpath(name, service string) string {
	if service == "" {
		service = "_"
	}
	return "/applications/" + name + "/services/" + service + "/env/"
}

func (api *APIClient) ApplicationEnviron(ctx context.Context, name, service string, all bool) (map[string]string, error) {
	var query url.Values
	if all {
		query = url.Values{}
		query.Set("all", "1")
	}

	var env map[string]string
	resp, err := api.cli.Get(ctx, envpath(name, service), query, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&env)
		resp.EnsureClosed()
	}
	return env, err
}

func (api *APIClient) ApplicationGetenv(ctx context.Context, name, service, key string) (string, error) {
	var env map[string]string
	resp, err := api.cli.Get(ctx, envpath(name, service)+key, nil, nil)
	if err == nil {
		err = json.NewDecoder(resp.Body).Decode(&env)
		resp.EnsureClosed()
	}
	return env[key], err
}

func (api *APIClient) ApplicationSetenv(ctx context.Context, name, service string, env map[string]string) error {
	resp, err := api.cli.Post(ctx, envpath(name, service), nil, env, nil)
	resp.EnsureClosed()
	return err
}

func (api *APIClient) ApplicationUnsetenv(ctx context.Context, name, service string, keys ...string) error {
	env := make(map[string]string)
	for _, k := range keys {
		env[k] = ""
	}

	query := url.Values{"remove": []string{""}}
	resp, err := api.cli.Post(ctx, envpath(name, service), query, env, nil)
	resp.EnsureClosed()
	return err
}
