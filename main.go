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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

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

const (
	MaxBytesRedis int64 = 50 << 20
	MaxBytesMinIO int64 = 1024 << 20
)

var (
	rdb           *redis.Client
	fileRdb       *redis.Client
	pdb           *pgxpool.Pool
	gcCtx         context.Context
	ctx           context.Context
	stop          context.CancelFunc
	gcStop        context.CancelFunc
	err           error
	endpoint      string = "minio-storage:9000"
	minioLogin    string = "cloud_admin"
	minioPassword string = "cloud_admin"
	pUser         string = "cloud_admin"
	pPassword     string = "cloud_admin_password"
	pHost         string = "postgre-db"
	pName         string = "cloud_storage_db"
)

func main() {
	r := gin.Default()

	gcCtx, gcStop = context.WithTimeout(context.Background(), 15*time.Second)
	defer gcStop()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	rdb = rediska.InitRedisForPasswords()
	fileRdb = rediska.InitRedisForCache()
	pdb, err = postgre.InitDB(gcCtx, pUser, pPassword, pHost, pName)
	if err != nil {
		slog.Error("ERR: Creating new postgreSQL database", "err", err)
		return
	}

	client, err := s3storage.InitMinIO(gcCtx, endpoint, minioLogin, minioPassword, "file")
	if err != nil {
		slog.Error("ERR: Creating new minIO client", "err", err)
		return
	}

	m := &CloudStorage{
		minClient:  client,
		fileBucket: "file",
	}

	pubilc := r.Group("/api/v1/auth")

	{
		pubilc.POST("/login", handleLogin)
	}

	private := r.Group("/api/v1")
	private.Use(AuthMiddleware())

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

	slog.Debug("[SERVER DEBUG]", "message", "We are going to start the server", "addr", ":11001")
	r.Run(":11001")
}

// Parses login and password from
// JSON form and creates new account in Redis
func handleLogin(c *gin.Context) {
	ip := c.RemoteIP()

	ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer stop()

	var Login InternalRequest

	if err := c.ShouldBindJSON(&Login); err != nil {
		slog.Debug("ERR: Parsing JSON", "err", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	bytess, err := bcrypt.GenerateFromPassword([]byte(Login.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Debug("ERR: Creating hash out of password", "err", err, "ip", ip)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	}

	password := string(bytess)

	status := rdb.Set(ctx, "user:"+Login.Username, password, 0).Err()
	if status != nil {
		if status == context.DeadlineExceeded {
			slog.Error("ERR: Timeout exeeded", "err", status, "ip", ip)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
			return
		}
		slog.Error("ERR: Writing in Redis", "err", status, "ip", ip)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	} else {
		c.String(http.StatusOK, "Account has been created and saved!", "login", Login.Username)
	}
}

// Explanation: handler gets multipart form from
// c.Request parses with help of c.FormFile and
// after that it writes in Redis if it is under 50 MiB
// and if not => writes in MinIO & PostgreSQL only without
// caching
func (m *CloudStorage) handleUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBytesMinIO)

	ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer stop()

	owner := c.GetHeader("login")

	fh, err := c.FormFile("file")
	if err != nil {
		slog.Error("ERR: Formating file", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	}

	if fh.Size > MaxBytesMinIO {
		slog.Debug("DEBUG: Client tried to save too lange file", "size", fh.Size)
		c.String(http.StatusRequestEntityTooLarge, "File must be under 1 GiB")
		return
	}

	networkFile, err := fh.Open()
	if err != nil {
		slog.Error("ERR: Timeout exeeded", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Unexpected error, ask support"})
		return
	}
	defer networkFile.Close()

	preparedStream, typeOfFile, err := core.DetectFileType(networkFile)
	if err != nil {
		slog.Error("ERR: Detecting file type", "err", err)
		c.String(http.StatusBadRequest, "File type is not allowed")
		return
	}

	var buf bytes.Buffer

	hasher := md5.New()

	mw := io.MultiWriter(&buf, hasher)

	_, err = io.Copy(mw, preparedStream)
	if err != nil {
		slog.Error("ERR: Copying in mw", "err", err)
		c.String(http.StatusBadRequest, "Unexpected error")
		return
	}

	filehash := hasher.Sum(nil)

	hash := hex.EncodeToString(filehash)

	if fh.Size < MaxBytesRedis {
		Query := Cache{
			Payload:   buf.Bytes(),
			OwnerNick: owner,
		}
		bytess, err := json.Marshal(Query)
		if err != nil {
			slog.Error("ERR: Marshaling struct for Redis", "err", err)
			c.String(http.StatusInternalServerError, "Unexpected error")
			return
		}

		if err = fileRdb.Set(ctx, hash, bytess, 120*time.Second).Err(); err != nil {
			slog.Error("ERR: Writing in Redis", "err", err)
			c.String(http.StatusInternalServerError, "Unexpected error")
			return
		}
	}

	_, err = m.minClient.PutObject(ctx, m.fileBucket, hash, &buf, int64(len(buf.Bytes())), minio.PutObjectOptions{ContentType: typeOfFile})
	if err != nil {
		slog.Error("ERR: Writing in MinIO", "err", err)
		c.String(http.StatusInternalServerError, "Unexpected error")
		return
	}

	ts := time.Now()

	err = postgre.AddData(ctx, owner, hash, ts)
	if err != nil {
		slog.Error("ERR: Writing in PostgreSQL", "err", err)
		c.String(http.StatusInternalServerError, "Unexpected error")
		return
	}

	c.String(http.StatusOK, "Your file has saved successfilly! Hash: "+hash)
}

// Explanation: handler gets hash of the file in ULR Link
// and firstly checks Redis and if there is no such file
// with that hash, program goes to minIO and takes data
// from there
func (m *CloudStorage) handleDownload(c *gin.Context) {
	ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer stop()

	hash := c.Param("h")
	owner := c.GetHeader("login")

	res, err := fileRdb.Get(ctx, hash).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			slog.Debug("DEBUG: Key does not exist in cache, checking internal storage")
		}
	} else {
		var SendDataToClient Cache

		err := json.Unmarshal(res, &SendDataToClient)
		if err != nil {
			slog.Error("ERR: Parsing data from Redis", "err", err)
			c.String(http.StatusInternalServerError, "Unexpected error")
			return
		}

		reader := bytes.NewReader(SendDataToClient.Payload)
		size := reader.Size()

		c.DataFromReader(http.StatusOK, size, "Content-Type application/octet-stream", reader, nil)
		return
	}

	err = postgre.DownloadData(ctx, owner, hash)
	if err != nil {
		slog.Error("ERR: Checking possibility to take file", "err", err)
		c.String(http.StatusForbidden, "Access denied!")
		return
	}

	object, err := m.minClient.GetObject(ctx, m.fileBucket, hash, minio.GetObjectOptions{})
	if err != nil {
		slog.Error("ERR: Taking file from minIO", "err", err)
		c.String(http.StatusBadRequest, "Unexpected error")
		return
	}
	defer object.Close()

	info, err := object.Stat()
	if err != nil {
		slog.Error("ERR: Taking size from minIO", "err", err)
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

	hash := c.Param("h")
	loggedOwner := c.GetHeader("login")

	err := postgre.RemoveData(ctx, loggedOwner, hash)
	if err != nil {
		slog.Error("ERR: Removing Data from PostgreSQL", "err", err)
		c.String(http.StatusBadRequest, "We can't remove this element from storage, might be that hash is invalid")
		return
	}

	res := fileRdb.Unlink(ctx, hash).Err()
	if res != nil {
		slog.Error("ERR: Removing Data from Redis", "err", res)
		c.String(http.StatusInternalServerError, "We can't remove this element from storage, internal error")
		return
	}

	err = m.minClient.RemoveObject(ctx, m.fileBucket, hash, minio.RemoveObjectOptions{})
	if err != nil {
		slog.Error("ERR: Removing Data from MinIO", "err", err)
		c.String(http.StatusInternalServerError, "We can't remove this element from storage, internal error")
		return
	}

	c.String(http.StatusOK, "File has been deleted successfully")
}

// removes account
func handleDeleteAccount(c *gin.Context) {
	ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer stop()

	owner := c.GetHeader("login")

	err := rdb.Unlink(ctx, owner).Err()
	if err != nil {
		slog.Error("ERR: Removing account", "login", owner)
		c.String(http.StatusInternalServerError, "Unexpected error")
		return
	}

	c.String(http.StatusOK, "Your account has been deleted successfully!")
	slog.Debug("User has deleted his account", "login", owner)
}

// AuthMiddleware checks http request for necessary headers
// like login and password
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, stop = context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer stop()

		loginStr := c.GetHeader("login")
		passwordStr := c.GetHeader("password")

		preparedLogin := fmt.Sprintf("user:%s", loginStr)

		res, err := rdb.Get(ctx, preparedLogin).Result()
		if err != nil {
			slog.Debug("DEBUG: Getting password from Redis", "err", err)
			c.String(http.StatusNotFound, "Incorrect login or inernal error")
			return
		}

		if err = bcrypt.CompareHashAndPassword([]byte(res), []byte(passwordStr)); err != nil {
			slog.Debug("DEBUG: Incorrect password", "err", err)
			c.String(http.StatusForbidden, "Incorrect login or password")
			return
		}

		c.Next()
	}
}
