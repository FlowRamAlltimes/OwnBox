# OwnBox
## Discription 
OwnBox is a self-hosted ligth-weight cloud storage which provides fast transportation of data with its own CLI client

<img width="319" height="290" alt="image" src="https://github.com/user-attachments/assets/c33963ed-192b-4a25-b738-214a59aa6f07" />


## Technology Stack
* Backend Core: Go + Gin + gRPC `github.com/gin-gonic/gin`
* Auth Core: Go + gRPC `google.golang.org/grpc`
* CLI Core: Go + `net/http`
* Database: PostgreSQL + pgx `github.com/jackc/pgx/v5`
* Cache: Redis `github.com/redis/go-redis/v9`
* Storage: MinIO `github.com/minio/minio-go/v7`
* Encryption: bcrypt + MD5 `golang.org/x/crypto/bcrypt`
* Observability: email alerting `github.com/jordan-wright/email`
* Monitoring: Prometheus + Grafana `github.com/prometheus/client_golang/prometheus`
* Cli: Cobra `github.com/spf13/cobra`

## Quick start

### Warning: This project has server and client parts but you can use native curl or other apps

#### Backend (Use VPS or local machine)

1. Clone my repo to your server
```bash
git clone https://github.com/FlowRamAlltimes/OwnBox
```
2. Download docker and docker compose if doesn't exist
3. Go to workdir
```bash
cd backend
```
5. Run application with docker compose:
```bash
docker compose up -d --build
```

#### CLI (Use on your machine)
1. Download latest release for your architecture(x86, arm)
2. Set Server ip and port in server.json configuration
3. Run CLI
```bash
./ownbox
```
#### Response:
```
Starts the CLI

Usage:
  ownbox [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  get         Here you can download data from OwnBox
  help        Help about any command
  login       creates account
  rm          removes file by hash
  rmacc       removes account
  upload      Uploads your file

Flags:
  -h, --help   help for ownbox

Use "ownbox [command] --help" for more information about a command.
```
# Grafana Dashboards Example
<img width="1756" height="986" alt="image" src="https://github.com/user-attachments/assets/3ce4e7c0-08bf-41b1-a508-71a44a4490f5" />


### Send me your suggestions about this project
 * any bugs
 * any logic mistakes 
