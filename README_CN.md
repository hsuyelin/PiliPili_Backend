<h1 align="center">PiliPili_Backend</h1>

<p align="center">A program suite for separating the frontend and backend of Emby service playback.</p>



![Commit Activity](https://img.shields.io/github/commit-activity/m/hsuyelin/PiliPili_Backend/main) ![Top Language](https://img.shields.io/github/languages/top/hsuyelin/PiliPili_Backend) ![Github License](https://img.shields.io/github/license/hsuyelin/PiliPili_Backend)



## 简介

1. 本项目是实现Emby媒体服务播前后端分离的后端程序, 需要与播放分离前端 [PiliPili播放前端](https://github.com/hsuyelin/PiliPili_Frontend) 一起配套使用。
2. 本程序大部分参考了 [YASS-Backend](https://github.com/FacMata/YASS-Backend)，并在其基础上进行了优化，使其更加易用。

------

## 原理

* 通过配合指定的`nginx`配置(可参考[nginx.conf](https://github.com/hsuyelin/PiliPili_Backend/blob/main/nginx/nginx.conf))，监听前端重定向播放链接的指定端口
* 从播放链接中解析出`path`和`signature`
* 通过签名解密出`signature`中包含的`mediaId`和`expireAt`
    * 如果正确解密出，则打印`mediaId`方便调试，同时通过`expireAt`判断是否过期，如果没过期则表示认证通过，如果过期了则返回`401`未认证
    * 如果未解密出，直接返回`401`未认证
* 通过配置文件中的`StorageBasePath`和播放链接解析出的`path`合并成一个本地路径
* 开始获取本地文件信息，如果未正确解析出返回`500`错误，如果正确解析出继续下一步
* 从客户端的请求头中获取`Content-Range`信息
    * 如果包含则代表是恢复播放，从需要播放的地方开始分片传输
    * 如果未包含则代表是从头播放，从文件开始的地方分片传输

![sequenceDiagram](https://github.com/hsuyelin/PiliPili_Backend/blob/main/img/sequenceDiagram_CN.png)

------

## 功能

* 支持目前所有版本的Emby服务器

* 支持请求多并发

* 支持签名解密，支持拦截过期播放链接


------

## 配置文件

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

* LogLevel：打印日志的等级
    * `WARN`：会显示所有的日志，除非开启`DEBUG`后也没办法满足需求，一般不建议使用这个等级的日志
    * `DEBUG`：会显示`DEBUG`/`INFO`/`ERROR`等级的日志，如果需要调试尽量使用这个等级的
    * `INFO`：显示`INFO`/`EROR`的日志，正常情况下使用这个等级可以满足需求
    * `ERROR`：如果接入后足够稳定，已经达到无人值守的阶段，可以使用这个等级，降低日志数量
* Encipher：加密因子，格式是`16`位长度的字符串，用于混淆签名，`前端和后端必须保持一致`
* StorageBasePath：
    * 前提：需要前端映射到Emby服务中存储路径和后端实际存储文件路径一致
    * 前端隐藏的目录前缀，例如：你前端的`EmbyPath`为`/mnt/anime/动漫/海贼王 (1999)/Season 22/37854 S22E1089 2160p.B-Global.mkv`，但是你想隐藏`/mnt`这个路径，你就在配置的`StorageBasePath`填写`/mnt`
* Server：
    * port: 需要监听的端口号，如果没有特殊需要，直接默认`60002`就可以了


------

## 如何使用

### 1. Docker安装(推荐)

#### 1.1 创建docker文件夹

```shell
mkdir -p /data/docker/pilipili_backend
```

#### 1.2 创建配置文件夹和配置文件

```shell
cd /data/docker/pilipili_backend
mkdir -p config && cd config
```

将 [config.yaml](https://github.com/hsuyelin/PiliPili_Backend/blob/main/config.yaml) 复制到`config`文件夹中，并进行编辑

#### 1.3 创建docker-compose.yaml

返回到 `/data/docker/pilipili_backend`目录，将 [docker-compose.yml](https://github.com/hsuyelin/PiliPili_Backend/blob/main/docker/docker-compose.yml) 复制到该目录下

#### 1.4 启动容器

```shell
docker-compose pull && docker-compose up -d
```

### 2. 手动安装

#### 2.1 安装Go环境

##### 2.1.1 卸载本机的Go程序

强制删除本机安装的go，为防止`go`版本不匹配

```shell
rm -rf /usr/local/go
```

##### 2.1.2 下载并安装最新版本的Go程序

```shell
wget -q -O /tmp/go.tar.gz https://go.dev/dl/go1.23.5.linux-amd64.tar.gz && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz
```

##### 2.1.3 将Go程序写入环境变量

```shell
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc && source ~/.bashrc
```

##### 2.1.4 验证是否安装成功

```shell
go version #显示 go version go1.23.5 linux/amd64 就是安装成功
```

#### 2.2 克隆后端程序组到本地

假如你需要克隆到`/data/emby_backend`这个目录

```shell
git clone https://github.com/hsuyelin/PiliPili_Backend.git /data/emby_backend
```

#### 2.3 进入后端程序目录编辑配置文件

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

#### 2.4 运行程序

```shell
nohup go run main.go config.yaml > streamer.log 2>&1 &
```