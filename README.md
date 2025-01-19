<h1 align="center">PiliPili_Backend</h1>

<p align="center">A program suite for separating the frontend and backend of Emby service playback.</p>



![Commit Activity](https://img.shields.io/github/commit-activity/m/hsuyelin/PiliPili_Backend/main) ![Top Language](https://img.shields.io/github/languages/top/hsuyelin/PiliPili_Backend) ![Github License](https://img.shields.io/github/license/hsuyelin/PiliPili_Backend)



[中文版本](https://github.com/hsuyelin/PiliPili_Backend/blob/main/README_CN.md)

### Introduction

1. This project is the backend application for separating Emby media service playback into frontend and backend components. It is designed to work with the playback frontend [PiliPili Playback Frontend](https://github.com/hsuyelin/PiliPili_Fronted).
2. This program is largely based on [YASS-Backend](https://github.com/FacMata/YASS-Backend), with optimizations made for improved usability.

------

### Principle

1. Use a specific `nginx`configuration (refer to [nginx.conf](https://github.com/hsuyelin/PiliPili_Backend/blob/main/nginx/nginx.conf) to listen on a designated port for redirected playback links from the frontend.

2. Parse the `path` and `signature` from the playback link.

3. Decrypt the `signature` to extract `mediaId`and `expireAt`:

    - If decryption succeeds, log the `mediaId` for debugging and validate the expiration time (`expireAt`). If valid, authentication passes; otherwise, return a `401 Unauthorized` error.
    - If decryption fails, immediately return a `401 Unauthorized` error.

4. Combine the `StorageBasePath` from the configuration file with the parsed `path` to generate a local file path.

5. Retrieve file information:

    - If retrieval fails, return a `500 Internal Server Error`.
    - If successful, proceed to the next step.

6. Extract the `Content-Range`

   from the client's request headers:

    - If present, resume playback from the specified range and stream the file in chunks.
    - If absent, start streaming from the beginning of the file.

   ![sequenceDiagram](https://github.com/hsuyelin/PiliPili_Backend/blob/main/img/sequenceDiagram.png)

------

### Features

- Compatible with all Emby server versions.
- Supports high-concurrency requests.
- Supports signature decryption and blocks expired playback links.

------

### Configuration File

```yaml
# Configuration for PiliPili Backend

# LogLevel defines the level of logging (e.g., INFO, DEBUG, ERROR)
LogLevel: "INFO"

# EncryptionKey is used for encryption and obfuscation of data.
Encipher: "vPQC5LWCN2CW2opz"

# StorageBasePath is the base directory where files are stored. This is a prefix for the storage paths.
StorageBasePath: "/mnt/anime/"

# Server configuration
Server:
  port: "60002"  # Port on which the server will listen
```

#### Key Settings:

1. **`LogLevel`**: Logging verbosity levels:
    - `WARN`: Minimal logging unless debugging is insufficient.
    - `DEBUG`: Logs `DEBUG`, `INFO`, and `ERROR`. Recommended for debugging.
    - `INFO`: Logs `INFO` and `ERROR`. Suitable for regular operations.
    - `ERROR`: For stable, unattended setups, minimizes log entries.
2. **`Encipher`**: A 16-character encryption key for signature obfuscation. **Must match between frontend and backend.**
3. **`StorageBasePath`**:
    - Ensures consistency between the frontend's Emby storage path mapping and the backend's actual file paths.
    - Example:
        - Frontend `EmbyPath`: `/mnt/anime/OnePiece/Season 22/file.mkv`.
        - If `/mnt` should be hidden, set `StorageBasePath: "/mnt"`.
        - Ensure the same path is configured in the [frontend](https://github.com/hsuyelin/PiliPili_Fronted).
4. **`Server` Configuration**:
    - `port`: Listening port, default is `60002`.

------

### Usage

#### Step 1: Install Go Environment

##### 1.1 Remove Existing Go Installation

```shell
rm -rf /usr/local/go
```

##### 1.2 Download and Install the Latest Go Version

```shell
wget -q -O /tmp/go.tar.gz https://go.dev/dl/go1.23.5.linux-amd64.tar.gz && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz
```

##### 1.3 Add Go to Environment Variables

```shell
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc
```

##### 1.4 Verify Installation

```shell
go version
# Expected output: go version go1.23.5 linux/amd64
```

------

#### Step 2: Clone the Backend Repository

Clone the repository into a directory, e.g., `/data/emby_backend`.

```shell
git clone https://github.com/hsuyelin/PiliPili_Backend.git /data/emby_backend
```

------

#### Step 3: Configure the Application

Edit the `config.yaml` file in the repository to match your setup.

------

#### Step 4: Run the Application

Run the program in the background:

```shell
nohup go run main.go config.yaml > streamer.log 2>&1 &
```