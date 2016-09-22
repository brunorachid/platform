package console

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cloudway/platform/auth/userdb"
	"github.com/cloudway/platform/broker"
	"github.com/cloudway/platform/config"
	"github.com/cloudway/platform/config/defaults"
	"github.com/cloudway/platform/console/auth"

	"gopkg.in/authboss.v0"
	_ "gopkg.in/authboss.v0/auth"
	_ "gopkg.in/authboss.v0/confirm"
	_ "gopkg.in/authboss.v0/recover"
	_ "gopkg.in/authboss.v0/register"
	//_ "gopkg.in/authboss.v0/remember"

	"github.com/Sirupsen/logrus"
	"github.com/aarondl/tpl"
	"github.com/gorilla/mux"
	"github.com/justinas/nosurf"
	"github.com/oxtoacart/bpool"
	"golang.org/x/net/context"
)

var funcs = template.FuncMap{
	"formatDate": func(date time.Time) string {
		return date.Format("2006/01/02 03:04pm")
	},
	"humanDuration": func(date time.Time) string {
		return humanDuration(time.Now().UTC().Sub(date))
	},
	"yield": func() string {
		return ""
	},
	"gravatar": func(email string, size int) string {
		hash := md5.Sum([]byte(email))
		id := hex.EncodeToString(hash[:])
		return fmt.Sprintf("https://cn.gravatar.com/avatar/%s?s=%d&d=mm&r=g", id, size)
	},
	"logo": func(tag, url string) template.URL {
		if url == "" {
			url = "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw=="
		} else if !(strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
			url = "/images/plugin/" + tag
		}
		return template.URL(url)
	},
}

type Console struct {
	*broker.Broker
	server    *http.Server
	listener  net.Listener
	ab        *authboss.Authboss
	templates tpl.Templates
	baseURL   *url.URL
}

func NewConsole(br *broker.Broker) (con *Console, err error) {
	con = &Console{Broker: br}

	rawurl := config.GetOrDefault("console.url", "http://api."+defaults.Domain())
	con.baseURL, err = url.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	err = con.setupAuthboss(br)
	if err != nil {
		return nil, err
	}

	funcs["download"] = func(os string) template.URL {
		dlurl := *con.baseURL
		dlurl.Path = fmt.Sprintf("/dist/%s/amd64/cwcli", os)
		if os == "windows" {
			dlurl.Path += ".exe"
		}
		return template.URL(dlurl.String())
	}

	viewRoot := filepath.Join(config.RootDir, "views", "console")
	con.templates = tpl.Must(tpl.Load(viewRoot, filepath.Join(viewRoot, "partials"), "layout.html.tpl", funcs))

	return con, nil
}

func (con *Console) Accept(addr string, listener net.Listener) {
	con.server = &http.Server{Addr: addr}
	con.listener = listener
}

func (con *Console) Serve() (err error) {
	m := mux.NewRouter()
	con.server.Handler = m
	con.InitRoutes(m)

	logrus.Infof("Console server listen on %s", con.listener.Addr())
	err = con.server.Serve(con.listener)
	if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
		err = nil
	}
	return err
}

func (con *Console) Close() {
	con.listener.Close()
}

func (con *Console) InitRoutes(m *mux.Router) {
	m.PathPrefix("/auth/").Handler(con.ab.NewRouter())

	dist := http.FileServer(http.Dir(filepath.Join(config.RootDir, "dist")))
	m.PathPrefix("/dist/").Handler(http.StripPrefix("/dist/", dist))

	static := http.FileServer(http.Dir(filepath.Join(config.RootDir, "static")))
	m.PathPrefix("/static/").Handler(http.StripPrefix("/static/", static))

	gets := m.Methods("GET").Subrouter()
	posts := m.Methods("POST").Subrouter()

	gets.HandleFunc("/", con.index)
	gets.HandleFunc("/password", con.password)
	posts.HandleFunc("/password", con.changePassword)

	gets.HandleFunc("/images/plugin/{tag:.*}", con.getPluginLogo)

	con.initSettingsRoutes(gets, posts)
	con.initApplicationsRoutes(gets, posts)
}

// General Email Regex (RFC 5322 Official Standard)
const _EMAIL_RE = `(?:[a-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+(?:\.[a-z0-9!#$%&'*+/=?^_` + "`" + `{|}~-]+)*|"(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21\x23-\x5b\x5d-\x7f]|\\[\x01-\x09\x0b\x0c\x0e-\x7f])*")@(?:(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?|\[(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?|[a-z0-9-]*[a-z0-9]:(?:[\x01-\x08\x0b\x0c\x0e-\x1f\x21-\x5a\x53-\x7f]|\\[\x01-\x09\x0b\x0c\x0e-\x7f])+)\])`

func (con *Console) setupAuthboss(br *broker.Broker) error {
	viewRoot := filepath.Join(config.RootDir, "views")

	ab := authboss.New()
	ab.Storer = auth.NewStorer(br)
	ab.MountPath = "/auth"
	ab.ViewsPath = filepath.Join(viewRoot, "auth")
	ab.RootURL = con.baseURL.String()
	ab.EmailFrom = config.GetOrDefault("smtp.from", "Cloudway <daemon@"+defaults.Domain()+">")

	b, err := ioutil.ReadFile(filepath.Join(viewRoot, "console", "layout.html.tpl"))
	if err != nil {
		return err
	}
	ab.Layout = template.Must(template.New("layout").Funcs(funcs).Parse(string(b)))
	ab.LayoutDataMaker = con.layoutData

	ab.XSRFName = "csrf_token"
	ab.XSRFMaker = func(_ http.ResponseWriter, r *http.Request) string {
		return nosurf.Token(r)
	}

	ab.CookieStoreMaker = auth.NewCookieStorer
	ab.SessionStoreMaker = auth.NewSessionStorer

	ab.Policies = []authboss.Validator{
		authboss.Rules{
			FieldName:       "email",
			Required:        true,
			AllowWhitespace: false,
			MustMatch:       regexp.MustCompile(_EMAIL_RE),
			MatchError:      "Please enter a valid email address",
		},
		authboss.Rules{
			FieldName:       "password",
			Required:        true,
			MinLength:       4,
			MaxLength:       20,
			AllowWhitespace: false,
		},
	}

	modules := []string{"auth", "register"}
	if ab.Mailer = initMailer(); ab.Mailer != nil {
		modules = append(modules, "confirm", "recover")
	}

	con.ab = ab
	return ab.Init(modules...)
}

func initMailer() authboss.Mailer {
	host := config.Get("smtp.host")
	port := config.Get("smtp.port")
	username := config.Get("smtp.username")
	password := config.Get("smtp.password")

	if host == "" || username == "" || password == "" {
		logrus.Warn("No SMTP server configured")
		if config.Debug {
			return authboss.LogMailer(os.Stdout)
		} else {
			return nil
		}
	}

	if port == "" {
		port = "25"
	}

	auth := smtp.PlainAuth("", username, password, host)
	return authboss.SMTPMailer(host+":"+port, auth)
}

func (con *Console) currentUser(w http.ResponseWriter, r *http.Request) *userdb.BasicUser {
	user, err := con.ab.CurrentUser(w, r)
	if err != nil && err != authboss.ErrUserNotFound {
		logrus.Error(err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}
	if user == nil {
		http.Redirect(w, r, "/auth/login", http.StatusFound)
		return nil
	}
	return user.(*auth.AuthbossUser).Basic()
}

func (con *Console) NewUserBroker(user userdb.User) *broker.UserBroker {
	return con.Broker.NewUserBroker(user, context.Background())
}

func (con *Console) layoutData(w http.ResponseWriter, r *http.Request) authboss.HTMLData {
	var user *userdb.BasicUser
	userInter, err := con.ab.CurrentUser(w, r)
	if userInter != nil && err == nil {
		user = userInter.(*auth.AuthbossUser).Basic()
	}
	return con.layoutUserData(w, r, user)
}

func (con *Console) layoutUserData(w http.ResponseWriter, r *http.Request, user *userdb.BasicUser) authboss.HTMLData {
	return authboss.HTMLData{
		"loggedin": user != nil,
		"user":     user,
		authboss.FlashSuccessKey: con.ab.FlashSuccess(w, r),
		authboss.FlashErrorKey:   con.ab.FlashError(w, r),
	}
}

func (con *Console) mustRender(w http.ResponseWriter, r *http.Request, name string, data authboss.HTMLData) {
	data.MergeKV("csrf_token", nosurf.Token(r))
	err := con.templates.Render(w, name, data)
	if err != nil {
		logrus.Error(err)
		http.Error(w, "Error occurred rendering template", http.StatusInternalServerError)
	}
}

var bufPool = bpool.NewBufferPool(10)

func (con *Console) error(w http.ResponseWriter, r *http.Request, status int, message string, returnPath string) {
	tmpl, ok := con.templates["error"]
	if !ok {
		logrus.Error("Template named 'error' does not exist")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	data := con.layoutData(w, r)
	data.MergeKV("csrf_token", nosurf.Token(r))
	data.MergeKV("error_message", message)
	data.MergeKV("return_path", returnPath)

	buf := bufPool.Get()
	defer bufPool.Put(buf)

	err := tmpl.ExecuteTemplate(buf, "", data)
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(status)
	io.Copy(w, buf)
}

func (con *Console) badRequest(w http.ResponseWriter, r *http.Request, err error, returnPath string) bool {
	if err == nil {
		return false
	} else {
		con.error(w, r, http.StatusBadRequest, fmt.Sprintf("Bad request: %s", err), returnPath)
		return true
	}
}

func (con *Console) index(w http.ResponseWriter, r *http.Request) {
	data := con.layoutData(w, r)
	if data["loggedin"].(bool) {
		if data["user"].(*userdb.BasicUser).Namespace == "" {
			http.Redirect(w, r, "/settings", http.StatusFound)
		} else {
			http.Redirect(w, r, "/applications", http.StatusFound)
		}
	} else {
		con.mustRender(w, r, "index", data)
	}
}

func (con *Console) password(w http.ResponseWriter, r *http.Request) {
	data := con.layoutData(w, r)
	con.mustRender(w, r, "password", data)
}

func (con *Console) changePassword(w http.ResponseWriter, r *http.Request) {
	user := con.currentUser(w, r)
	if user == nil {
		return
	}

	oldPassword := r.FormValue("oldPassword")
	newPassword := r.FormValue("newPassword")
	confirmPassword := r.FormValue("confirmPassword")

	data := con.layoutUserData(w, r, user)
	if newPassword != confirmPassword {
		data.MergeKV("error", "新密码与确认密码不匹配")
		con.mustRender(w, r, "password", data)
		return
	}

	err := con.Users.ChangePassword(user.Name, oldPassword, newPassword)
	if err != nil {
		data.MergeKV("error", err)
		con.mustRender(w, r, "password", data)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func (con *Console) getPluginLogo(w http.ResponseWriter, r *http.Request) {
	tag := mux.Vars(r)["tag"]
	meta, err := con.Hub.GetPluginInfo(tag)
	if meta == nil || err != nil {
		http.NotFound(w, r)
		return
	}

	url := meta.Logo
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		http.Error(w, "Invalid icon path", http.StatusInternalServerError)
		return
	}

	b, err := ioutil.ReadFile(filepath.Join(meta.Path, url))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctype := mime.TypeByExtension(filepath.Ext(url))
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}

func humanDuration(d time.Duration) string {
	if seconds := int(d.Seconds()); seconds < 1 {
		return "不到一秒钟前"
	} else if seconds < 60 {
		return fmt.Sprintf("%d 秒钟前", seconds)
	} else if minutes := int(d.Minutes()); minutes == 1 {
		return "大约一分钟前"
	} else if minutes < 60 {
		return fmt.Sprintf("%d 分钟前", minutes)
	} else if hours := int(d.Hours()); hours == 1 {
		return "大约一小时前"
	} else if hours < 48 {
		return fmt.Sprintf("%d 小时前", hours)
	} else if hours < 24*7*2 {
		return fmt.Sprintf("%d 天前", hours/24)
	} else if hours < 24*30*3 {
		return fmt.Sprintf("%d 周前", hours/24/7)
	} else if hours < 24*365*2 {
		return fmt.Sprintf("%d 个月前", hours/24/30)
	} else {
		return fmt.Sprintf("%d 年前", int(d.Hours())/24/365)
	}
}
