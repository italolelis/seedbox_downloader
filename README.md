# Seedbox Downloader

![Build](https://github.com/italolelis/seedbox_downloader/actions/workflows/main.yml/badge.svg)
![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)
![Go Version](https://img.shields.io/badge/Go-1.22-blue)

> **Disclaimer**
>
> This project is provided for educational and legal use only. The author does **not** incentivize, condone, or support piracy or the illegal downloading, sharing, or distribution of copyrighted material. **Do not use this software to download, share, or manage any content for which you do not have the legal rights.**
>
> It is your responsibility to ensure that your use of this software complies with all applicable laws and regulations in your jurisdiction. The author assumes no liability for any misuse of this project.

---

## Table of Contents
- [Features](#features)
- [Quick Start](#quick-start)
- [Docker Compose Example](#docker-compose-example)
- [Contributing](#contributing)
- [Community](#community)
- [Project Structure](#project-structure)
- [Setup Instructions](#setup-instructions)
- [Usage](#usage)
- [Docker](#docker)
- [License](#license)
- [Environment Variables](#environment-variables)
- [Put.io Integration](#putio-integration)

## Features

- 🏷️  Download files from Deluge by tag
- 🗂️  Tracks downloads in SQLite
- 🐳  Lightweight Docker & distroless support
- 🔒  Secure, minimal, and easy to deploy
- 🧪  Automated tests and CI/CD
- 🧩  Modular client system (add more clients in `internal/dc`)
- 🔄  Put.io proxy for *Arr integration

## Quick Start

### Deluge Integration
```sh
docker run --rm \
  -e DELUGE_BASE_URL=<url> \
  -e DELUGE_API_URL_PATH=<path> \
  -e DELUGE_USERNAME=<username> \
  -e DELUGE_PASSWORD=<password> \
  -e TARGET_LABEL=<label> \
  -e DOWNLOAD_DIR=<download_dir> \
  -e KEEP_DOWNLOADED_FOR=168h \
  -e DOWNLOAD_CLIENT=deluge \
  ghcr.io/italolelis/seedbox_downloader:latest
```

### Put.io Integration
```sh
docker run --rm \
  -e PUTIO_TOKEN=<token> \
  -e TARGET_LABEL=<label> \
  -e DOWNLOAD_DIR=/downloads \
  -e PUTIO_BASE_DIR=/downloads \
  -e KEEP_DOWNLOADED_FOR=168h \
  -e DOWNLOAD_CLIENT=putio \
  -e TRANSMISSION_USERNAME=<username> \
  -e TRANSMISSION_PASSWORD=<password> \
  -e WEB_BIND_ADDRESS=0.0.0.0:9091 \
  -p 9091:9091 \
  ghcr.io/italolelis/seedbox_downloader:latest
```

## Docker Compose Example

You can use Docker Compose for easier configuration and management:

```yaml
services:
  # Deluge integration for syncing downloads
  seedbox_downloader:
    image: ghcr.io/italolelis/seedbox_downloader:latest
    container_name: seedbox_downloader
    environment:
      DELUGE_BASE_URL: "https://your-deluge-url"
      DELUGE_API_URL_PATH: "/deluge/json"
      DELUGE_USERNAME: "your-username"
      DELUGE_PASSWORD: "your-password"
      TARGET_LABEL: "your-label"
      DOWNLOAD_DIR: "/downloads"
      KEEP_DOWNLOADED_FOR: "168h"
      DOWNLOAD_CLIENT: "deluge"
    volumes:
      - downloads:/downloads
    restart: unless-stopped

  # Put.io proxy for *Arr integration
  putioarr:
    image: ghcr.io/italolelis/seedbox_downloader:latest
    container_name: putioarr
    environment:
      PUTIO_TOKEN: "your-putio-token"
      PUTIO_INSECURE: "false"               # Optional
      TARGET_LABEL: "your-label"           # Required for organizing downloads
      DOWNLOAD_DIR: "/downloads"           # Local directory where files will be downloaded
      PUTIO_BASE_DIR: "/downloads"         # Base directory in Put.io where files will be stored
      KEEP_DOWNLOADED_FOR: "168h"          # Keep files for 7 days (in hours)
      DOWNLOAD_CLIENT: "putio"             # Required to use Put.io client
      TRANSMISSION_USERNAME: "username"     # Required for proxy authentication
      TRANSMISSION_PASSWORD: "password"     # Required for proxy authentication
      WEB_BIND_ADDRESS: "0.0.0.0:9091"     # Web server bind address
      WEB_READ_TIMEOUT: "30s"              # Web server read timeout
      WEB_WRITE_TIMEOUT: "30s"             # Web server write timeout
      WEB_IDLE_TIMEOUT: "5s"               # Web server idle timeout
      WEB_SHUTDOWN_TIMEOUT: "30s"          # Web server shutdown timeout
      SONARR_API_KEY: "your-sonarr-api-key" # Optional: Sonarr integration
      SONARR_BASE_URL: "http://sonarr:8989" # Optional: Sonarr API URL
      RADARR_API_KEY: "your-radarr-api-key" # Optional: Radarr integration
      RADARR_BASE_URL: "http://radarr:7878" # Optional: Radarr API URL
    ports:
      - "9091:9091"
    restart: unless-stopped

volumes:
  downloads:
```

You can also use a `.env` file to manage environment variables. See the [Docker documentation](https://docs.docker.com/compose/environment-variables/) for more details.

## Contributing

Contributions are welcome! Please open issues or pull requests for improvements, bug fixes, or new features. For major changes, please open an issue first to discuss what you would like to change.

- **Linting:** This project uses [golangci-lint](https://golangci-lint.run/) with configuration in `.golangci.yml`. Run `golangci-lint run` locally to check your code before submitting a PR.
- **Go version:** The project uses Go 1.22. Please use this version for development and CI.

## Community

- [GitHub Issues](https://github.com/italolelis/seedbox_downloader/issues): Ask questions, report bugs, or suggest features.

---

## Project Structure

```
seedbox_downloader
├── cmd
│   └── seedbox_downloader
│       └── main.go          # Entry point of the application
├── internal
│   ├── config
│   │   └── config.go        # Configuration loading and struct
│   ├── dc
│   │   ├── client.go        # DownloadClient interface and TorrentInfo struct
│   │   └── deluge
│   │       ├── client.go    # Deluge API client for interacting with files
│   │       └── client_test.go
│   ├── downloader
│   │   └── downloader.go    # Logic for downloading files and tracking in DB
│   ├── storage
│   │   ├── storage.go       # Storage interfaces
│   │   └── sqlite
│   │       ├── init.go
│   │       ├── read_repository.go
│   │       └── write_repository.go
│   ├── cleanup
│   │   └── cleanup.go       # Cleanup logic for old downloads
│   ├── logctx
│   │   └── logctx.go        # Logging context helpers
├── Dockerfile               # Instructions for building the Docker image
├── go.mod                   # Go module definition
├── go.sum                   # Module dependency checksums
├── .golangci.yml            # Linter configuration
└── README.md                # Project documentation
```

## Setup Instructions

1. **Clone the repository:**
   ```
   git clone <repository-url>
   cd seedbox_downloader
   ```

2. **Set up environment variables:**
   Ensure the following environment variables are set in your environment:
   - `DELUGE_BASE_URL`: Base URL for the Deluge API.
   - `DELUGE_API_URL_PATH`: API URL path for Deluge.
   - `DELUGE_USERNAME`: Username for Deluge authentication.
   - `DELUGE_PASSWORD`: Password for Deluge authentication.
   - `TARGET_LABEL`: Label for filtering downloaded files.
   - `DELUGE_COMPLETED_DIR`: Directory for completed downloads in Deluge.
   - `TARGET_DIR`: Directory where files will be downloaded.
   - `KEEP_DOWNLOADED_FOR`: Duration to keep downloaded files (e.g., "7d").

3. **Build the application:**
   ```
   go build -o seedbox_downloader ./cmd/seedbox_downloader
   ```

4. **Run the application:**
   ```
   ./seedbox_downloader
   ```

## Usage

The application will start polling the Deluge API at the specified interval, downloading files tagged with the specified label, and tracking downloads in the SQLite database.

## Docker

To build a Docker image for the application, use the provided `Dockerfile`. The image is built using a distroless base for a lean production environment.

1. **Build the Docker image:**
   ```
   docker build -t seedbox_downloader .
   ```

2. **Run the Docker container:**
   ```
   docker run -e DELUGE_BASE_URL=<url> -e DELUGE_API_URL_PATH=<path> -e DELUGE_USERNAME=<username> -e DELUGE_PASSWORD=<password> -e TARGET_LABEL=<label> -e DELUGE_COMPLETED_DIR=<completed_dir> -e TARGET_DIR=<target_dir> -e KEEP_DOWNLOADED_FOR=<duration> seedbox_downloader
   ```

## License

This project is licensed under the MIT License. See the LICENSE file for details.

## Environment Variables

The application is configured via environment variables. Below is a list of all supported variables:

| Variable                  | Required | Default         | Description                                                                 |
|---------------------------|----------|-----------------|-----------------------------------------------------------------------------|
| DELUGE_BASE_URL           | Yes*     |                 | Base URL for the Deluge API. Required for Deluge client.                    |
| DELUGE_API_URL_PATH       | Yes*     |                 | API URL path for Deluge (e.g., `/deluge/json`). Required for Deluge client. |
| DELUGE_USERNAME           | Yes*     |                 | Username for Deluge authentication. Required for Deluge client.             |
| DELUGE_PASSWORD           | Yes*     |                 | Password for Deluge authentication. Required for Deluge client.             |
| TARGET_LABEL              | Yes      |                 | Label for filtering downloaded files.                                       |
| DOWNLOAD_DIR              | Yes      |                 | Directory where files will be downloaded.                                   |
| KEEP_DOWNLOADED_FOR       | No       | 24h             | Duration to keep downloaded files (e.g., `7d`, `24h`).                      |
| POLLING_INTERVAL          | No       | 10m             | How often to poll for new downloads.                                        |
| CLEANUP_INTERVAL          | No       | 10m             | How often to run cleanup of old downloads.                                  |
| LOG_LEVEL                 | No       | INFO            | Logging level: `DEBUG`, `INFO`, `WARN`, `ERROR`.                            |
| DISCORD_WEBHOOK_URL       | No       |                 | If set, sends notifications to this Discord webhook.                        |
| DB_PATH                   | No       | downloads.db    | Path to the SQLite database file.                                           |
| MAX_PARALLEL              | No       | 5               | Maximum number of parallel downloads.                                       |
| DOWNLOAD_CLIENT           | No       | deluge          | Download client to use: `deluge` or `putio`.                                |
| PUTIO_TOKEN               | Yes*     |                 | Your Put.io API token. Required for Put.io client.                          |
| PUTIO_BASE_DIR           | Yes*     |                 | Base directory in Put.io where files will be stored. Required for Put.io client. |
| PUTIO_INSECURE           | No       | false           | Allow insecure connections for Put.io client.                               |
| TRANSMISSION_USERNAME     | Yes*     |                 | Username for proxy authentication. Required for Put.io client.              |
| TRANSMISSION_PASSWORD     | Yes*     |                 | Password for proxy authentication. Required for Put.io client.              |
| WEB_BIND_ADDRESS         | No       | 0.0.0.0:9091    | Address to bind the web server to.                                          |
| WEB_READ_TIMEOUT         | No       | 30s             | Read timeout for the web server.                                            |
| WEB_WRITE_TIMEOUT        | No       | 30s             | Write timeout for the web server.                                           |
| WEB_IDLE_TIMEOUT         | No       | 5s              | Idle timeout for the web server.                                            |
| WEB_SHUTDOWN_TIMEOUT     | No       | 30s             | Shutdown timeout for the web server.                                        |
| SONARR_API_KEY           | No       |                 | API key for Sonarr integration.                                             |
| SONARR_BASE_URL          | No       |                 | Base URL for Sonarr API.                                                    |
| RADARR_API_KEY           | No       |                 | API key for Radarr integration.                                             |
| RADARR_BASE_URL          | No       |                 | Base URL for Radarr API.                                                    |

> **Note:** Variables marked with * are required depending on which download client you're using (Deluge or Put.io).

## Put.io Integration

The application provides a proxy service that acts as a bridge between your *Arr applications (Sonarr, Radarr, Whisparr, etc.) and Put.io. This proxy handles the communication between *Arr and Put.io, manages downloads, and ensures proper file organization. *Arr applications will automatically import the downloaded files once they're ready.

### Configuration

To use Put.io as your download client, you'll need to set the following environment variables:

| Variable              | Required | Description                                    |
|-----------------------|----------|------------------------------------------------|
| PUTIO_TOKEN           | Yes      | Your Put.io API token                          |
| PUTIO_INSECURE        | No       | Allow insecure connections (default: false)    |
| PROXY_USERNAME        | Yes      | Username for *Arr authentication               |
| PROXY_PASSWORD        | Yes      | Password for *Arr authentication               |
| TARGET_LABEL          | Yes      | Label for organizing downloads in Put.io       |
| TARGET_DIR            | Yes      | Local directory where files will be downloaded |
| PUTIO_BASE_DIR        | Yes      | Base directory in Put.io where files will be stored |
| KEEP_DOWNLOADED_FOR   | Yes      | How long to keep downloaded files (in hours)   |
| DOWNLOAD_CLIENT       | Yes      | Must be set to "putio" to use Put.io client    |
| TRANSMISSION_USERNAME | Yes      | Username for proxy authentication              |
| TRANSMISSION_PASSWORD | Yes      | Password for proxy authentication              |

### Directory Structure

The Put.io integration uses two directory settings:
- `PUTIO_BASE_DIR`: This is the directory in your Put.io account where files will be stored. For example, if set to `/downloads`, all files will be stored in this directory in your Put.io account.
- `TARGET_DIR`: This is the local directory on your system where downloaded files will be stored. This is where *Arr applications will look for completed downloads.

For example, if you set:
- `PUTIO_BASE_DIR=/downloads`
- `TARGET_DIR=/downloads`

Files will be:
1. Downloaded to `/downloads` in your Put.io account
2. Then downloaded to `/downloads` on your local system
3. *Arr will look for them in the local `/downloads` directory

### *Arr Integration

1. **Configure the Proxy:**
   ```yaml
   services:
     putioarr:
       image: ghcr.io/italolelis/seedbox_downloader:latest
       container_name: putioarr
       environment:
         PUTIO_TOKEN: "your-putio-token"
         PUTIO_INSECURE: "false"               # Optional
         PROXY_USERNAME: "your-username"       # Required for *Arr
         PROXY_PASSWORD: "your-password"       # Required for *Arr
         TARGET_LABEL: "your-label"           # Required for organizing downloads
         TARGET_DIR: "/downloads"             # Local directory where files will be downloaded
         PUTIO_BASE_DIR: "/downloads"         # Base directory in Put.io where files will be stored
         KEEP_DOWNLOADED_FOR: "168h"          # Keep files for 7 days (in hours)
         DOWNLOAD_CLIENT: "putio"             # Required to use Put.io client
         TRANSMISSION_USERNAME: "username"     # Required for proxy authentication
         TRANSMISSION_PASSWORD: "password"     # Required for proxy authentication
       ports:
         - "9091:9091"
       restart: unless-stopped
   ```

2. **Configure *Arr Applications:**
   In your *Arr application (Sonarr, Radarr, Whisparr, etc.), add a new download client with these settings:

   - **Type:** Transmission
   - **Name:** Put.io
   - **Host:** `http://your-server:9091`
   - **Port:** `9091`
   - **URL Base:** `/transmission`
   - **Username:** `<your configured PROXY_USERNAME>`
   - **Password:** `<your configured PROXY_PASSWORD>`
   - **Category:** `your-putio-folder` (must be an existing folder in your Put.io account)

3. **Important Notes:**
   - The proxy emulates a Transmission client, so *Arr applications will treat it as such
   - The category you specify must be an existing folder in your Put.io account
   - The proxy will automatically handle file downloads and organization
   - *Arr will automatically import the downloaded files once they're ready
   - The proxy handles authentication and file management automatically

4. **Security Considerations:**
   - Always use HTTPS in production
   - Use strong passwords for the proxy
   - Consider using a reverse proxy with SSL termination
   - Keep your Put.io token secure and never share it

5. **Troubleshooting:**
   - If downloads fail, check the Put.io API token
   - Ensure the specified category folder exists in your Put.io account
   - Check the proxy logs for detailed error messages
   - Verify network connectivity between *Arr and the proxy
   - Make sure the proxy has proper permissions to access Put.io

## Deluge Integration

The Deluge integration is focused on syncing downloads from your Deluge server. Unlike the Put.io integration, it doesn't require a proxy as *Arr applications have native support for Deluge. This integration is useful for users who want to keep their downloads in sync with their Deluge server.
