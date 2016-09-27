package applications

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudway/platform/api/server/httputils"
	"github.com/cloudway/platform/api/server/router"
	"github.com/cloudway/platform/api/types"
	"github.com/cloudway/platform/auth/userdb"
	"github.com/cloudway/platform/broker"
	"github.com/cloudway/platform/config"
	"github.com/cloudway/platform/config/defaults"
	"github.com/cloudway/platform/container"
	"github.com/cloudway/platform/hub"
	"github.com/cloudway/platform/pkg/manifest"
	"github.com/cloudway/platform/pkg/serverlog"
	"github.com/cloudway/platform/scm"
)

const appPath = "/applications/{name:[^/]+}"
const servicePath = appPath + "/services/{service:[^/]+}"

type applicationsRouter struct {
	*broker.Broker
	routes []router.Route
}

func NewRouter(broker *broker.Broker) router.Router {
	r := &applicationsRouter{Broker: broker}

	r.routes = []router.Route{
		router.NewGetRoute("/applications/", r.list),
		router.NewPostRoute("/applications/", r.create),
		router.NewGetRoute(appPath, r.info),
		router.NewDeleteRoute(appPath, r.delete),
		router.NewPostRoute(appPath+"/start", r.start),
		router.NewPostRoute(appPath+"/stop", r.stop),
		router.NewPostRoute(appPath+"/restart", r.restart),
		router.NewGetRoute(appPath+"/status", r.status),
		router.NewGetRoute("/applications/status/", r.allStatus),
		router.NewGetRoute(appPath+"/procs", r.procs),
		router.NewGetRoute(appPath+"/stats", r.stats),
		router.NewPostRoute(appPath+"/deploy", r.deploy),
		router.NewGetRoute(appPath+"/deploy", r.getDeployments),
		router.NewGetRoute(appPath+"/repo", r.download),
		router.NewPutRoute(appPath+"/repo", r.upload),
		router.NewGetRoute(appPath+"/data", r.dump),
		router.NewPutRoute(appPath+"/data", r.restore),
		router.NewPostRoute(appPath+"/scale", r.scale),
		router.NewPostRoute(appPath+"/services/", r.createService),
		router.NewDeleteRoute(servicePath, r.removeService),
		router.NewGetRoute(servicePath+"/env/", r.environ),
		router.NewPostRoute(servicePath+"/env/", r.setenv),
		router.NewGetRoute(servicePath+"/env/{key:.*}", r.getenv),
	}

	return r
}

func (ar *applicationsRouter) Routes() []router.Route {
	return ar.routes
}

func (ar *applicationsRouter) NewUserBroker(r *http.Request) *broker.UserBroker {
	ctx := r.Context()
	user := httputils.UserFromContext(ctx)
	return ar.Broker.NewUserBroker(user, ctx)
}

func (ar *applicationsRouter) list(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	apps, err := ar.NewUserBroker(r).GetApplications()
	if err != nil {
		return err
	}

	var names []string
	for name := range apps {
		names = append(names, name)
	}
	return httputils.WriteJSON(w, http.StatusOK, names)
}

func (ar *applicationsRouter) info(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	var (
		br        = ar.NewUserBroker(r)
		name      = vars["name"]
		namespace = br.Namespace()
		app       *userdb.Application
	)

	apps, err := br.GetApplications()
	if err != nil {
		return err
	}

	if app = apps[name]; app == nil {
		return httputils.NewStatusError(http.StatusNotFound)
	}

	info, err := ar.getInfo(name, namespace, app)
	if err != nil {
		return err
	}

	cs, _ := ar.FindApplications(r.Context(), name, namespace)
	info.Scaling = len(cs)

	return httputils.WriteJSON(w, http.StatusOK, &info)
}

func (ar *applicationsRouter) getInfo(name, namespace string, app *userdb.Application) (info *types.ApplicationInfo, err error) {
	info = &types.ApplicationInfo{
		Name:      name,
		Namespace: namespace,
		CreatedAt: app.CreatedAt,
		Scaling:   1,
	}

	base, err := url.Parse(defaults.ApiURL())
	if err != nil {
		return
	}

	host, port := base.Host, ""
	if i := strings.IndexRune(host, ':'); i != -1 {
		host, port = host[:i], host[i:]
	}
	info.URL = fmt.Sprintf("%s://%s-%s.%s%s", base.Scheme, name, namespace, defaults.Domain(), port)
	info.SSHURL = fmt.Sprintf("ssh://%s-%s@%s%s", name, namespace, host, ":2200") // FIXME

	info.SCMType = ar.SCM.Type()
	cloneURL := config.Get("scm.clone_url")
	if cloneURL != "" {
		cloneURL = strings.Replace(cloneURL, "<namespace>", namespace, -1)
		cloneURL = strings.Replace(cloneURL, "<repo>", name, -1)
		info.CloneURL = cloneURL
	}

	for _, tag := range app.Plugins {
		p, err := ar.Hub.GetPluginInfo(tag)
		if err == nil {
			p.Path = ""
			if p.Category.IsFramework() {
				info.Framework = p
			} else {
				info.Services = append(info.Services, p)
			}
		}
	}

	return
}

var namePattern = regexp.MustCompile("^[a-z][a-z_0-9]*$")

func (ar *applicationsRouter) create(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}
	if err := httputils.CheckForJSON(r); err != nil {
		return err
	}

	br := ar.NewUserBroker(r)

	var req types.CreateApplication
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return err
	}

	opts := container.CreateOptions{
		Name:    req.Name,
		Repo:    req.Repo,
		Scaling: 1,
		Log:     serverlog.New(w),
	}

	if !namePattern.MatchString(opts.Name) {
		msg := "The application name can only contains lower case letters, digits or underscores."
		http.Error(w, msg, http.StatusBadRequest)
		return nil
	}

	if req.Framework == "" {
		msg := "The application framework cannot be empty."
		http.Error(w, msg, http.StatusBadRequest)
		return nil
	}

	tags := append([]string{req.Framework}, req.Services...)

	app, cs, err := br.CreateApplication(opts, tags)
	if err != nil {
		serverlog.SendError(w, err)
		return nil
	}

	if err = br.StartContainers(cs, opts.Log); err != nil {
		serverlog.SendError(w, err)
		return nil
	}

	if info, err := ar.getInfo(req.Name, br.Namespace(), app); err != nil {
		serverlog.SendError(w, err)
	} else {
		serverlog.SendObject(w, info)
	}

	return nil
}

func (ar *applicationsRouter) delete(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	br := ar.NewUserBroker(r)

	if err := br.RemoveApplication(vars["name"]); err != nil {
		return err
	} else {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

func (ar *applicationsRouter) createService(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.CheckForJSON(r); err != nil {
		return err
	}

	br := ar.NewUserBroker(r)

	var tags []string
	if err := json.NewDecoder(r.Body).Decode(&tags); err != nil {
		return err
	}

	opts := container.CreateOptions{
		Name: vars["name"],
		Log:  serverlog.New(w),
	}

	cs, err := br.CreateServices(opts, tags)
	if err != nil {
		serverlog.SendError(w, err)
		return nil
	}

	if err := br.StartContainers(cs, opts.Log); err != nil {
		serverlog.SendError(w, err)
		return nil
	}

	return nil
}

func (ar *applicationsRouter) removeService(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	br := ar.NewUserBroker(r)

	name, service := vars["name"], vars["service"]
	if err := br.RemoveService(name, service); err != nil {
		return err
	} else {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

func (ar *applicationsRouter) start(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	err := ar.NewUserBroker(r).StartApplication(vars["name"], serverlog.New(w))
	if err != nil {
		serverlog.SendError(w, err)
	}
	return nil
}

func (ar *applicationsRouter) stop(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return ar.NewUserBroker(r).StopApplication(vars["name"])
}

func (ar *applicationsRouter) restart(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	err := ar.NewUserBroker(r).RestartApplication(vars["name"], serverlog.New(w))
	if err != nil {
		serverlog.SendError(w, err)
	}
	return nil
}

func (ar *applicationsRouter) status(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	var (
		br   = ar.NewUserBroker(r)
		name = vars["name"]
	)
	if err := br.Refresh(); err != nil {
		return err
	}
	status, err := ar.getStatus(r.Context(), name, br.Namespace())
	if err != nil {
		return err
	}
	return httputils.WriteJSON(w, http.StatusOK, status)
}

func (ar *applicationsRouter) allStatus(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	br := ar.NewUserBroker(r)
	if err := br.Refresh(); err != nil {
		return err
	}

	var (
		apps      = br.User.Basic().Applications
		namespace = br.Namespace()
		status    = map[string][]*types.ContainerStatus{}
		mu        sync.Mutex
		wg        sync.WaitGroup
	)
	wg.Add(len(apps))
	for name := range apps {
		go func(name string, wg *sync.WaitGroup) {
			defer wg.Done()
			st, err := ar.getStatus(r.Context(), name, namespace)
			if err == nil {
				mu.Lock()
				status[name] = st
				mu.Unlock()
			}
		}(name, &wg)
	}
	wg.Wait()
	return httputils.WriteJSON(w, http.StatusOK, status)
}

func (ar *applicationsRouter) getStatus(ctx context.Context, name, namespace string) ([]*types.ContainerStatus, error) {
	cs, err := ar.FindAll(ctx, name, namespace)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, broker.ApplicationNotFoundError(name)
	}

	status := make([]*types.ContainerStatus, len(cs))
	for i, c := range cs {
		st := &types.ContainerStatus{}
		plugin := ar.initContainerJSON(c, &st.ContainerJSONBase)
		status[i] = st

		st.IPAddress = c.IP()
		st.State = c.ActiveState(ctx)
		if plugin != nil {
			st.Ports = plugin.GetPrivatePorts()
		}

		started, err := time.Parse(time.RFC3339Nano, c.StartedAt())
		if err == nil {
			st.Uptime = int64(time.Now().UTC().Sub(started))
		}
	}
	return status, nil
}

func (ar *applicationsRouter) initContainerJSON(c container.Container, data *types.ContainerJSONBase) *manifest.Plugin {
	data.ID = c.ID()
	data.Category = c.Category()

	tag := c.PluginTag()
	plugin, err := ar.Hub.GetPluginInfo(tag)

	if err == nil {
		data.Name = plugin.Name
		data.DisplayName = plugin.DisplayName
	} else {
		_, _, pn, pv, _ := hub.ParseTag(tag)
		data.Name = pn
		data.DisplayName = pn + " " + pv
	}

	if c.ServiceName() != "" {
		data.Name = c.ServiceName()
	}

	return plugin
}

func (ar *applicationsRouter) procs(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	var (
		ctx  = r.Context()
		br   = ar.NewUserBroker(r)
		name = vars["name"]
	)

	if err := br.Refresh(); err != nil {
		return err
	}

	cs, err := ar.FindAll(ctx, name, br.Namespace())
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return broker.ApplicationNotFoundError(name)
	}

	procs := make([]*types.ProcessList, 0, len(cs))
	for _, c := range cs {
		if ps, err := c.Processes(ctx); err == nil {
			proc := &types.ProcessList{}
			ar.initContainerJSON(c, &proc.ContainerJSONBase)
			proc.Processes = ps.Processes
			proc.Headers = ps.Headers
			procs = append(procs, proc)
		}
	}
	return httputils.WriteJSON(w, http.StatusOK, procs)
}

func (ar *applicationsRouter) stats(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	w.Header().Set("Content-Type", "application/x-json-stream")
	err := ar.NewUserBroker(r).Stats(vars["name"], w)
	if err != nil {
		w.Header().Del("Content-Type")
		return err
	}
	return nil
}

func (ar *applicationsRouter) deploy(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	user := httputils.UserFromContext(r.Context())
	name, branch := vars["name"], r.FormValue("branch")

	err := ar.Deploy(name, user.Namespace, branch, serverlog.New(w))
	if err != nil {
		serverlog.SendError(w, err)
	}
	return nil
}

func (ar *applicationsRouter) getDeployments(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	user := httputils.UserFromContext(r.Context())
	name := vars["name"]

	current, err := ar.SCM.GetDeploymentBranch(user.Namespace, name)
	if err != nil {
		return err
	}
	branches, err := ar.SCM.GetDeploymentBranches(user.Namespace, name)
	if err != nil {
		return err
	}

	resp := types.Deployments{
		Current:  convertBranchJson(current),
		Branches: convertBranchesJson(branches),
	}

	return httputils.WriteJSON(w, http.StatusOK, &resp)
}

func convertBranchJson(br *scm.Branch) *types.Branch {
	return &types.Branch{
		Id:        br.Id,
		DisplayId: br.DisplayId,
		Type:      br.Type,
	}
}

func convertBranchesJson(branches []*scm.Branch) []*types.Branch {
	result := make([]*types.Branch, len(branches))
	for i := range branches {
		result[i] = convertBranchJson(branches[i])
	}
	return result
}

func (ar *applicationsRouter) download(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	tr, err := ar.NewUserBroker(r).Download(vars["name"])
	if err != nil {
		return err
	}
	defer tr.Close()

	w.Header().Set("Content-Type", "application/tar+gzip") // TODO: parse Accept header
	w.WriteHeader(http.StatusOK)

	zw := gzip.NewWriter(w)
	if _, err = io.Copy(zw, tr); err == nil {
		err = zw.Close()
	}
	return err
}

func (ar *applicationsRouter) upload(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}

	_, binary := r.Form["binary"]

	err := ar.NewUserBroker(r).Upload(vars["name"], r.Body, binary, serverlog.New(w))
	if err != nil {
		serverlog.SendError(w, err)
	}
	return nil
}

func (ar *applicationsRouter) dump(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	tr, err := ar.NewUserBroker(r).Dump(vars["name"])
	if err != nil {
		return err
	}
	defer tr.Close()

	w.Header().Set("Content-Type", "application/tar+gzip")
	w.WriteHeader(http.StatusOK)

	zw := gzip.NewWriter(w)
	if _, err = io.Copy(zw, tr); err == nil {
		err = zw.Close()
	}
	return err
}

func (ar *applicationsRouter) restore(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return ar.NewUserBroker(r).Restore(vars["name"], r.Body)
}

func (ar *applicationsRouter) scale(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	user := httputils.UserFromContext(r.Context())
	name := vars["name"]
	scaling := r.FormValue("scale")

	var up, down bool
	if strings.HasPrefix(scaling, "+") {
		up = true
		scaling = scaling[1:]
	} else if strings.HasPrefix(scaling, "-") {
		down = true
		scaling = scaling[1:]
	}

	num, err := strconv.Atoi(scaling)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}

	if up || down {
		cs, err := ar.FindApplications(r.Context(), name, user.Namespace)
		if err != nil {
			return err
		}
		if up {
			num = len(cs) + num
		} else {
			num = len(cs) - num
		}
	}

	br := ar.NewUserBroker(r)
	cs, err := br.ScaleApplication(name, num)
	if err != nil {
		return err
	}

	err = br.StartContainers(cs, serverlog.New(w))
	if err != nil {
		serverlog.SendError(w, err)
	}
	return nil
}

func (ar *applicationsRouter) getContainers(ctx context.Context, namespace string, vars map[string]string) (cs []container.Container, err error) {
	name, service := vars["name"], vars["service"]
	if service == "" || service == "_" {
		cs, err = ar.FindApplications(ctx, name, namespace)
		if err == nil && len(cs) == 0 {
			err = broker.ApplicationNotFoundError(name)
		}
	} else {
		cs, err = ar.FindService(ctx, name, namespace, service)
		if err == nil && len(cs) == 0 {
			err = fmt.Errorf("Service '%s' not found in application '%s'", service, name)
		}
	}
	return cs, err
}

func (ar *applicationsRouter) getContainer(ctx context.Context, namespace string, vars map[string]string) (container.Container, error) {
	cs, err := ar.getContainers(ctx, namespace, vars)
	if err == nil {
		return cs[0], nil
	} else {
		return nil, err
	}
}

func (ar *applicationsRouter) environ(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	ctx := r.Context()
	user := httputils.UserFromContext(ctx)

	container, err := ar.getContainer(ctx, user.Namespace, vars)
	if err != nil {
		return err
	}

	opt := "env"
	if _, all := r.Form["all"]; all {
		opt = "env-all"
	}
	if info, err := container.GetInfo(ctx, opt); err != nil {
		return err
	} else {
		return httputils.WriteJSON(w, http.StatusOK, info.Env)
	}
}

func (ar *applicationsRouter) getenv(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	ctx := r.Context()
	user := httputils.UserFromContext(ctx)

	container, err := ar.getContainer(ctx, user.Namespace, vars)
	if err != nil {
		return err
	}
	if info, err := container.GetInfo(ctx, "env"); err != nil {
		return err
	} else {
		key := vars["key"]
		val := info.Env[key]
		return httputils.WriteJSON(w, http.StatusOK, map[string]string{key: val})
	}
}

var validEnvKey = regexp.MustCompile(`^[a-zA-Z_0-9]+$`)

func (ar *applicationsRouter) setenv(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	if err := httputils.ParseForm(r); err != nil {
		return err
	}
	if err := httputils.CheckForJSON(r); err != nil {
		return err
	}

	_, rm := r.Form["remove"]
	var env map[string]string
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		return err
	}

	ctx := r.Context()
	user := httputils.UserFromContext(ctx)

	cs, err := ar.getContainers(ctx, user.Namespace, vars)
	if err != nil {
		return err
	}

	args := []string{"/usr/bin/cwctl", "setenv"}
	if rm {
		args = append(args, "-d")
		for k := range env {
			args = append(args, k)
		}
	} else {
		args = append(args, "--export")
		for k, v := range env {
			if !validEnvKey.MatchString(k) {
				http.Error(w, k+": Invalid environment variable key", http.StatusBadRequest)
				return nil
			}
			args = append(args, k+"="+v)
		}
	}

	for _, container := range cs {
		if err = container.ExecE(ctx, "root", nil, nil, args...); err != nil {
			return err
		}
	}

	return nil
}
