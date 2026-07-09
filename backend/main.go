package main

import (
	"bytes"
	"cloud-storage/core"
	postgre "cloud-storage/datab"
	rediska "cloud-storage/internal"
	s3storage "cloud-storage/storage"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type ctxKey string

const ReqID ctxKey = "request-id-key"

type InternalRequest struct {
	Username string `json:"login" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type CloudStorage struct {
	minClient  *minio.Client
	fileBucket string
}

type Cache struct {
	Payload   []byte `json:"payload"`
	OwnerNick string `json:"owner"`
}

type ConfigurationYml struct {
	Postgresql postgresCfg `yaml:"postgres"`
	Redis      redisCfg    `yaml:"redis"`
	MinIO      minioCfg    `yaml:"minio"`
	Server     serverCfg   `yaml:"server"`
	Mail       mailCfg     `yaml:"alert"`
}

type postgresCfg struct {
	Name     string `yaml:"name"`
	Host     string `yaml:"host"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type redisCfg struct {
	PasswordsAddr string `yaml:"password_addr"`
	CacheAddr     string `yaml:"cache_addr"`
	Limit         int    `yaml:"max_limit"`
	TimeToCache   int    `yaml:"time_to_cache"`
}

type minioCfg struct {
	Endpoint string `yaml:"endpoint"`
	Login    string `yaml:"login"`
	Password string `yaml:"password"`
	Limit    int    `yaml:"max_limit"`
	Bucket   string `yaml:"bucket"`
}

type serverCfg struct {
	Addr string `yaml:"address"`
}

type mailCfg struct {
	AdminEmail string `yaml:"admin_mail"`
	Addr       string `yaml:"smtp_addr"`
	Port       string `yaml:"smtp_port"`
	Sender     string `yaml:"sender_mail"`
	Password   string `yaml:"sender_password"`
}

var (
	rdb               *redis.Client
	fileRdb           *redis.Client
	pdb               *pgxpool.Pool
	gcCtx             context.Context
	ctx               context.Context
	stop              context.CancelFunc
	gcStop            context.CancelFunc
	err               error
	ConfigurationFile string = "config.yml"
	Cfg               *ConfigurationYml
	counter           uint64
	Cached_hits       = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cloud_storage_cached_hits",
		Help: "total cached hits with Redis",
	})
	Active_files = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cloud_storage_active_files",
		Help: "total files which contains server now",
	})
	TotalConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cloud_storage_connections_now",
		Help: "total connections now",
	})
	TotalErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cloud_srorage_total_errors",
		Help: "total errors for this session",
	})
	TotalAccounts = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cloud_storage_total_accounts",
		Help: "total accounts counter",
	})
)

func main() {
	r := gin.Default()

	gcCtx, gcStop = context.WithTimeout(context.Background(), 15*time.Second)
	defer gcStop()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	bytesRes, err := os.ReadFile(ConfigurationFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Error("ERR: File does not exist", "err", err)
			return
		}
		slog.Error("ERR: Reading File", "err", err)
		return
	}

	Cfg, err = ParseYMLConfig(bytesRes)
	if err != nil {
		slog.Error("ERR: Unmarshaling data into struct", "err", err)
		return
	}

	rdb = rediska.InitRedisForPasswords(Cfg.Redis.PasswordsAddr)
	fileRdb = rediska.InitRedisForCache(Cfg.Redis.CacheAddr)

	var E *postgre.CustomError

	pdb, err = postgre.InitDB(gcCtx, Cfg.Postgresql.User, Cfg.Postgresql.Password, Cfg.Postgresql.Host, Cfg.Postgresql.Name)
	if errors.As(err, &E) {
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, E.Op, "CRITICAL", E.Err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is dead", "err", err)
		}
		slog.Error("ERR: "+E.Op, "err", E.Err)
		return
	}

	client, err := s3storage.InitMinIO(gcCtx, Cfg.MinIO.Endpoint, Cfg.MinIO.Login, Cfg.MinIO.Password, Cfg.MinIO.Bucket)
	if err != nil {
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Creating new minIO client", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is dead", "err", err)
		}
		slog.Error("ERR: Creating new minIO client", "err", err)
		return
	}

	m := &CloudStorage{
		minClient:  client,
		fileBucket: Cfg.MinIO.Bucket,
	}

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	pubilc := r.Group("/api/v1/auth")

	{
		pubilc.POST("/login", handleLogin)
	}

	private := r.Group("/api/v1")
	private.Use(AuthMiddleware(), AddRequestID())

	authGroup := private.Group("/auth")

	{
		authGroup.DELETE("/delete", handleDeleteAccount)
	}

	file := private.Group("/file")

	{
		file.GET("/download/:h", m.handleDownload)
		file.POST("/upload", m.handleUpload)
		file.DELETE("/delete/:h", m.handleDelete)
	}

	slog.Debug("[SERVER DEBUG]", "message", "We are going to start the server", "addr", Cfg.Server.Addr)
	if err = r.Run(Cfg.Server.Addr); err != nil {
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Removing data from PostgreSQL", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "err", err)
		}
		slog.Error("Server is down")
	}
}

func handleLogin(c *gin.Context) {

	idIface := c.Request.Context().Value(ReqID)
	id, ok := idIface.(string)
	if !ok {
		id = "unknown"
	}

	ip := c.RemoteIP()

	ctx, stop = context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer stop()

	var Login InternalRequest

	if err := c.ShouldBindJSON(&Login); err != nil {
		slog.Debug("ERR: Parsing JSON", "id", id, "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	bytess, err := bcrypt.GenerateFromPassword([]byte(Login.Password), bcrypt.DefaultCost)
	if err != nil {
		TotalErrors.Inc()
		slog.Debug("ERR: Creating hash out of password", "id", id, "err", err, "ip", ip)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	}

	password := string(bytess)

	status := rdb.Set(ctx, "user:"+Login.Username, password, 0).Err()
	if status != nil {
		TotalErrors.Inc()
		if status == context.DeadlineExceeded {
			if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Redis timeout exceeded", "CRITICAL", status.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
				slog.Error("ERR: Alerter is dead", "id", id, "err", err)
			}
			slog.Error("ERR: Timeout exeeded", "id", id, "err", status, "ip", ip)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
			return
		}
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Creating new minIO client", "CRITICAL", status.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is dead", "id", id, "err", err)
		}
		slog.Error("ERR: Writing in Redis", "id", id, "err", status, "ip", ip)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	} else {
		c.String(http.StatusOK, "Account has been created and saved! Username: "+Login.Username)
		TotalAccounts.Inc()
	}
}

// Explanation: handler gets multipart form from
// c.Request parses with help of c.FormFile and
// after that it writes in Redis if it is under 50 MiB
// and if not => writes in MinIO & PostgreSQL only without
// caching
func (m *CloudStorage) handleUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(Cfg.MinIO.Limit)<<20)

	id := c.Request.Context().Value(ReqID).(string)

	var E *postgre.CustomError

	ctx, stop = context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer stop()

	owner := c.GetHeader("login")

	fh, err := c.FormFile("file")
	if err != nil {
		TotalErrors.Inc()
		slog.Error("ERR: Formating file", "id", id, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	}

	if fh.Size > int64(Cfg.MinIO.Limit)<<20 {
		slog.Debug("DEBUG: Client tried to save too lange file", "id", id, "size", fh.Size)
		c.String(http.StatusRequestEntityTooLarge, "File must be under 1 GiB")
		return
	}

	networkFile, err := fh.Open()
	if err != nil {
		TotalErrors.Inc()
		slog.Error("ERR: Timeout exeeded", "id", id, "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	}
	defer networkFile.Close()

	var CoreErr *core.CustomError

	preparedStream, typeOfFile, err := core.DetectFileType(networkFile)
	if errors.As(err, &CoreErr) {
		TotalErrors.Inc()
		slog.Error("ERR: "+CoreErr.Op, "id", id, "err", CoreErr.Err)
		c.String(CoreErr.Code, CoreErr.Msg)
		return
	}

	var buf bytes.Buffer

	hasher := md5.New()

	mw := io.MultiWriter(&buf, hasher)

	_, err = io.Copy(mw, preparedStream)
	if err != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Creating new minIO client", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: Copying in mw", "id", id, "err", err)
		c.String(http.StatusInternalServerError, "Unexpected error")
		return
	}

	filehash := hasher.Sum(nil)

	hash := hex.EncodeToString(filehash)

	if fh.Size < int64(Cfg.Redis.Limit)<<20 {
		Query := Cache{
			Payload:   buf.Bytes(),
			OwnerNick: owner,
		}
		bytess, err := json.Marshal(Query)
		if err != nil {
			TotalErrors.Inc()
			if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Marshaling data from Json", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
				slog.Error("ERR: Alerter is down", "id", id, "err", err)
			}
			slog.Error("ERR: Marshaling struct for Redis", "id", id, "err", err)
			c.String(http.StatusInternalServerError, "Unexpected error")
			return
		}

		if err = fileRdb.Set(ctx, hash, bytess, time.Duration(Cfg.Redis.TimeToCache)*time.Second).Err(); err != nil {
			TotalErrors.Inc()
			if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Setting data in Redis", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
				slog.Error("ERR: Alerter is dead", "id", id, "err", err)
			}
			slog.Error("ERR: Writing in Redis", "id", id, "err", err)
			c.String(http.StatusInternalServerError, "Unexpected error")
			return
		}
	}

	_, err = m.minClient.PutObject(ctx, m.fileBucket, hash, &buf, int64(len(buf.Bytes())), minio.PutObjectOptions{ContentType: typeOfFile})
	if err != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Writing in minIO", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: Writing in MinIO", "id", id, "err", err)
		c.String(http.StatusInternalServerError, "Unexpected error")
		return
	}

	ts := time.Now()

	err = postgre.AddData(ctx, owner, hash, ts)
	if errors.As(err, &E) {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Adding data", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is dead", "id", id, "err", err)
		}
		slog.Error("ERR: "+E.Op, "id", id, "err", E.Err)
		c.String(E.Code, E.Msg)
		return
	}

	Active_files.Inc()
	c.String(http.StatusOK, "Your file has saved successfilly! Hash: "+hash)
}

// Explanation: handler gets hash of the file in ULR Link
// and firstly checks Redis and if there is no such file
// with that hash, program goes to minIO and takes data
// from there
func (m *CloudStorage) handleDownload(c *gin.Context) {
	ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer stop()

	id := c.Request.Context().Value(ReqID).(string)

	var E *postgre.CustomError

	hash := c.Param("h")
	owner := c.GetHeader("login")

	res, err := fileRdb.Get(ctx, hash).Bytes()
	if err != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Taking data from Redis", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is dead", "id", id, "err", err)
		}
		if errors.Is(err, redis.Nil) {
			slog.Debug("DEBUG: Key does not exist in cache, checking internal storage", "id", id)
		}
	} else {
		var SendDataToClient Cache

		err := json.Unmarshal(res, &SendDataToClient)
		if err != nil {
			TotalErrors.Inc()
			if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Unmarshaling data", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
				slog.Error("ERR: Alerter is dead", "id", id, "err", err)
			}
			slog.Error("ERR: Parsing data from Redis", "id", id, "err", err)
			c.String(http.StatusInternalServerError, "Unexpected error")
			return
		}

		reader := bytes.NewReader(SendDataToClient.Payload)
		size := reader.Size()

		c.DataFromReader(http.StatusOK, size, "Content-Type application/octet-stream", reader, nil)
		Cached_hits.Inc()
		return
	}

	err = postgre.DownloadData(ctx, owner, hash)
	if errors.As(err, &E) {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Taking data from PostgreSQL", "CRITICAL", E.Err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: "+E.Op, "id", id, "err", E.Err)
		c.String(http.StatusForbidden, E.Msg)
		return
	}

	object, err := m.minClient.GetObject(ctx, m.fileBucket, hash, minio.GetObjectOptions{})
	if err != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Taking file from minIO", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: Taking file from minIO", "id", id, "err", err)
		c.String(http.StatusBadRequest, "Unexpected error")
		return
	}
	defer object.Close()

	info, err := object.Stat()
	if err != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Taking stats from minIO", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: Taking size from minIO", "id", id, "err", err)
		c.String(http.StatusBadRequest, "Unexpected error")
		return
	}

	c.DataFromReader(http.StatusOK, info.Size, "application/octet-stream", object, nil)
}

// function gets hash of target and checks for abilitiy
// to remove file from storage(if real owner mismatches with logged owner)
func (m *CloudStorage) handleDelete(c *gin.Context) {
	ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer stop()

	id := c.Request.Context().Value(ReqID).(string)

	var E *postgre.CustomError

	hash := c.Param("h")
	loggedOwner := c.GetHeader("login")

	err := postgre.RemoveData(ctx, loggedOwner, hash)
	if errors.As(err, &E) {
		if E.Code == 500 {
			TotalErrors.Inc()
			if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Removing data from PostgreSQL", "CRITICAL", E.Err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
				slog.Error("ERR: Alerter is down", "id", id, "err", err)
			}
		} else {
			slog.Error("ERR: "+E.Op, "id", id, "err", E.Err)
			c.String(E.Code, E.Msg)
			return
		}
	}

	res := fileRdb.Unlink(ctx, hash).Err()
	if res != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Removing data from Redis", "CRITICAL", res.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: Removing Data from Redis", "id", id, "err", res)
		c.String(http.StatusInternalServerError, "We can't remove this element from storage, internal error")
		return
	}

	err = m.minClient.RemoveObject(ctx, m.fileBucket, hash, minio.RemoveObjectOptions{})
	if err != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Removing data from MinIO", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: Removing Data from MinIO", "id", id, "err", err)
		c.String(http.StatusInternalServerError, "We can't remove this element from storage, internal error")
		return
	}

	Active_files.Dec()
	c.String(http.StatusOK, "File has been deleted successfully")
}

// removes account
func handleDeleteAccount(c *gin.Context) {
	ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer stop()

	id := c.Request.Context().Value(ReqID).(string)

	owner := c.GetHeader("login")

	err := rdb.Unlink(ctx, owner).Err()
	if err != nil {
		TotalErrors.Inc()
		if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Unlinking data from Redis", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
			slog.Error("ERR: Alerter is down", "id", id, "err", err)
		}
		slog.Error("ERR: Removing account", "id", id, "login", owner)
		c.String(http.StatusInternalServerError, "Unexpected error")
		return
	}

	c.String(http.StatusOK, "Your account has been deleted successfully!")
	TotalAccounts.Dec()
	slog.Debug("User has deleted his account", "id", id, "login", owner)
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		TotalConnections.Inc()
		ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer stop()

		ip := c.RemoteIP()

		loginStr := c.GetHeader("login")
		passwordStr := c.GetHeader("password")

		preparedLogin := fmt.Sprintf("user:%s", loginStr)

		res, err := rdb.Get(ctx, preparedLogin).Result()
		if err != nil {
			TotalErrors.Inc()
			if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Taking data from Redis", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
				slog.Error("ERR: Alerter is down", "ip", ip, "err", err)
			}
			slog.Debug("DEBUG: Getting password from Redis", "ip", ip, "err", err)
			c.String(http.StatusNotFound, "Incorrect login or inernal error")
			return
		}

		if err = bcrypt.CompareHashAndPassword([]byte(res), []byte(passwordStr)); err != nil {
			TotalErrors.Inc()
			if err = core.CriticalAlerter(Cfg.Mail.AdminEmail, "Comparison of hash and password", "CRITICAL", err.Error(), Cfg.Mail.Addr, Cfg.Mail.Port, Cfg.Mail.Sender, Cfg.Mail.Password); err != nil {
				slog.Error("ERR: Alerter is down", "ip", ip, "err", err)
			}
			slog.Debug("DEBUG: Incorrect password", "ip", ip, "err", err)
			c.String(http.StatusForbidden, "Incorrect login or password")
			return
		}

		c.Next()
		TotalConnections.Dec()
	}
}

func AddRequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := atomic.AddUint64(&counter, 1)
		reqIDstr := fmt.Sprintf("REQ-ID=%v", reqID)

		newCtx := context.WithValue(c.Request.Context(), ReqID, reqIDstr)

		c.Request = c.Request.WithContext(newCtx)

		c.Next()
	}
}

func ParseYMLConfig(stream []byte) (*ConfigurationYml, error) {
	var TmpStruct *ConfigurationYml

	if err := yaml.Unmarshal(stream, &TmpStruct); err != nil {
		TotalErrors.Inc()
		return nil, err
	}
	return TmpStruct, nil
}
