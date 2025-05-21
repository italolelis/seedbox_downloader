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

## Features

- 🏷️  Download files from Deluge by tag
- 🗂️  Tracks downloads in SQLite
- 🐳  Lightweight Docker & distroless support
- 🔒  Secure, minimal, and easy to deploy
- 🧪  Automated tests and CI/CD
- 🧩  Modular client system (add more clients in `internal/dc`)

## Quick Start

```sh
docker run --rm \
  -e DELUGE_BASE_URL=<url> \
  -e DELUGE_API_URL_PATH=<path> \
  -e DELUGE_USERNAME=<username> \
  -e DELUGE_PASSWORD=<password> \
  -e TARGET_LABEL=<label> \
  -e DELUGE_COMPLETED_DIR=<completed_dir> \
  -e TARGET_DIR=<target_dir> \
  -e KEEP_DOWNLOADED_FILES_FOR=7d \
  ghcr.io/italolelis/seedbox_downloader:latest
```

## Docker Compose Example

You can use Docker Compose for easier configuration and management:

```yaml
services:
  seedbox_downloader:
    image: ghcr.io/italolelis/seedbox_downloader:latest
    container_name: seedbox_downloader
    environment:
      DELUGE_BASE_URL: "https://your-deluge-url"
      DELUGE_API_URL_PATH: "/deluge/json"
      DELUGE_USERNAME: "your-username"
      DELUGE_PASSWORD: "your-password"
      TARGET_LABEL: "your-label"
      DELUGE_COMPLETED_DIR: "/deluge/completed"
      TARGET_DIR: "/downloads"
      KEEP_DOWNLOADED_FILES_FOR: "7d"
    volumes:
      - downloads:/downloads
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
   - `KEEP_DOWNLOADED_FILES_FOR`: Duration to keep downloaded files (e.g., "7d").

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
   docker run -e DELUGE_BASE_URL=<url> -e DELUGE_API_URL_PATH=<path> -e DELUGE_USERNAME=<username> -e DELUGE_PASSWORD=<password> -e TARGET_LABEL=<label> -e DELUGE_COMPLETED_DIR=<completed_dir> -e TARGET_DIR=<target_dir> -e KEEP_DOWNLOADED_FILES_FOR=<duration> seedbox_downloader
   ```

## License

This project is licensed under the MIT License. See the LICENSE file for details.
