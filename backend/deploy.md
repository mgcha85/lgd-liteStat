# 오프라인 서버 배포 가이드 (Dev 버전)

이 문서는 인터넷 접속이 불가능한 **오프라인 서버(폐쇄망)**에 현재 개발 중인 애플리케이션(Dev 버전)을 배포하는 방법을 설명합니다. 오프라인 서버에서는 `go mod`, `pip install`, `apt-get` 등의 명령어를 사용하여 의존성을 다운로드할 수 없으므로, **개발 장비(온라인)**에서 모든 의존성이 포함된 Docker 이미지를 빌드한 후 옮겨야 합니다.

---

## 1. 준비 단계 (온라인 개발 장비)

소스 코드와 라이브러리가 모두 포함된 Docker 이미지를 빌드합니다.

### 1.1 Docker 이미지 빌드
프로젝트 루트 폴더(`lgd-liteStat`)에서 아래 명령어들을 실행하세요.

```bash
# 1. Backend 이미지 빌드
docker build -t lgd-backend:dev -f backend/Dockerfile backend/

# 2. Python Scheduler 이미지 빌드
docker build -t lgd-scheduler:dev -f python-scheduler/Dockerfile python-scheduler/

# 3. Frontend 이미지 빌드 (필요한 경우)
# docker build -t lgd-frontend:dev -f frontend/Dockerfile frontend/
```

### 1.2 이미지를 파일로 추출 (Export)
빌드된 이미지들을 하나의 `.tar` 파일로 묶어서 저장합니다.

```bash
docker save -o lgd-dev-images.tar lgd-backend:dev lgd-scheduler:dev
# frontend도 포함하려면 뒤에 lgd-frontend:dev 추가
```

### 1.3 배포 패키지 구성
배포를 위한 폴더(예: `deploy_package`)를 만들고 다음 파일들을 복사해 넣습니다:

1.  `lgd-dev-images.tar` (방금 만든 이미지 파일)
2.  `docker-compose.offline.yml` (아래 내용을 참고하여 새로 생성)
3.  `backend/config.yaml` -> `config.yaml`로 이름 변경하여 복사
4.  `backend/.env` (또는 오프라인 환경에 맞는 설정 파일)
5.  `python-scheduler/.env` (별도로 있다면 포함)

#### `docker-compose.offline.yml` 작성
오프라인 환경에서는 이미지를 빌드(`build: .`)하지 않고, 로드된 이미지를 바로 사용(`image: ...`)하도록 설정해야 합니다. 아래 내용을 복사해서 파일을 만드세요.

```yaml
version: '3.8'

services:
  backend:
    image: lgd-backend:dev
    container_name: lgd-litestat-backend
    ports:
      - "8082:8082"
    volumes:
      - ./data:/app/data
      - ./.env:/app/.env
      - ./config.yaml:/app/config.yaml
    environment:
      - DB_PATH=/app/data/analytics.duckdb
    restart: unless-stopped
    networks:
      - litestat-offline-net

  scheduler:
    image: lgd-scheduler:dev
    container_name: lgd-litestat-scheduler
    volumes:
      - ./data:/app/data
      - ./scheduler.env:/app/.env  # python-scheduler 설정 파일
      - ./config.yaml:/app/config.yaml
    environment:
      - DATA_DIR=/app/data/lake
    restart: unless-stopped
    networks:
      - litestat-offline-net

networks:
  litestat-offline-net:
    driver: bridge
```

---

## 2. 서버로 파일 전송

준비된 `deploy_package` 폴더를 USB, 사내 파일 전송 시스템, 또는 SCP 등을 사용하여 오프라인 서버로 복사합니다.

---

## 3. 배포 및 실행 (오프라인 서버)

### 3.1 Docker 이미지 로드 (Load)
전송받은 폴더로 이동하여 이미지 파일을 Docker에 로드합니다.

```bash
cd deploy_package
docker load -i lgd-dev-images.tar
```
*성공 시 "Loaded image: lgd-backend:dev" 등의 메시지가 출력됩니다.*

### 3.2 애플리케이션 실행
데이터 폴더를 생성하고 서비스를 시작합니다.

```bash
# 데이터 저장용 디렉토리 생성 (필요 시)
mkdir -p data

# 서비스 시작 (오프라인용 compose 파일 사용)
docker-compose -f docker-compose.offline.yml up -d
```

### 3.3 상태 확인
로그를 확인하여 정상적으로 실행되었는지 점검합니다.
```bash
docker logs -f lgd-litestat-backend
```

---

## 4. (옵션) 소스 코드만 전송하여 오프라인 빌드하기

**"이미지를 매번 옮기는 것이 번거롭고, 소스 코드만 수정해서 바로 반영하고 싶습니다."**

가능합니다. 단, 인터넷이 안 되므로 `go mod download`가 작동하지 않습니다. 따라서 **의존성(라이브러리)을 포함(Vendor)**하여 가져가야 합니다.

### 4.1 사전 준비 (최초 1회)
오프라인 서버에 **베이스 이미지**는 반드시 있어야 합니다. 최초 1회만 아래 이미지를 옮겨두세요.
```bash
# 온라인 PC에서 실행
docker pull golang:1.24-bookworm
docker pull debian:bookworm-slim
docker save -o base-images.tar golang:1.24-bookworm debian:bookworm-slim python:3.13-slim

# 오프라인 서버로 전송 후 로드
docker load -i base-images.tar
```

### 4.2 소스 코드 준비 (온라인 PC)
Go 모듈의 의존성 라이브러리를 `vendor` 폴더에 다운로드합니다. 이 폴더도 함께 복사해야 합니다.

```bash
cd backend
go mod tidy
go mod vendor
# 이제 backend/vendor 폴더에 모든 라이브러리가 저장되었습니다.
```

### 4.3 파일 전송
`lgd-liteStat` 프로젝트 폴더(소스 코드 + `vendor` 포함)를 통째로 오프라인 서버로 복사합니다.

### 4.4 오프라인 빌드 실행
오프라인 서버에서 `Dockerfile.offline`을 사용하여 빌드합니다.

1.  **Backend 빌드**
    ```bash
    cd backend
    docker build -t lgd-backend:dev -f Dockerfile.offline .
    # 인터넷 없이 vendor 폴더를 사용하여 빌드됩니다.
    ```

2.  **Scheduler 빌드** (Python도 비슷하게 offline용 처리 필요하지만, 위에서 만든 이미지 사용 권장)
    *   *Python은 vendor 개념이 복잡하므로(whl 파일 필요), Python Scheduler는 기존 방식(이미지 전송)을 권장합니다.*
    *   또는 `pip download -d ./packages -r requirements.txt`로 패키지를 받아가서 설치해야 합니다.

3.  **실행**
    ```bash
    cd ..
    docker-compose -f docker-compose.offline.yml up -d --build
    # docker-compose.offline.yml 파일에서 image: lgd-backend:dev 를 사용하므로
    # 위에서 빌드한 태그(lgd-backend:dev)가 그대로 사용됩니다.
    ```

---

## 5. 데이터 이관 방법

### 기존 데이터(DuckDB) 이관 (Dev -> Offline)
개발 장비의 데이터를 오프라인 서버로 옮기고 싶은 경우:
1.  개발 장비에서 서비스 중지.
2.  **데이터 압축**:
    ```bash
    tar -czvf data_backup.tar.gz data/
    ```
3.  `data_backup.tar.gz` 파일을 오프라인 서버로 전송.
4.  **압축 해제**:
    ```bash
    tar -xzvf data_backup.tar.gz
    # docker-compose가 바라보는 ./data 경로에 맞게 위치 조정
    ```
5.  서비스 재시작.
