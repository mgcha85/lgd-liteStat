# LGD liteStat 튜토리얼

본 문서는 LGD liteStat 시스템의 기동 방법, API 사용법, 그리고 과거 데이터(Backfill) 적재 방법을 안내합니다.

## 1. 시스템 기동 방법 (Getting Started)

LGD liteStat은 Docker(또는 Podman) 컨테이너 기반으로 배포됩니다. 환경에 따라 적절한 방법을 선택하세요.

### 1.1 온라인 환경 (Basic Usage)
Docker Compose를 사용하여 간단하게 기동할 수 있습니다.

```bash
# 최신 이미지 빌드 및 실행
docker-compose -f docker-compose.prod.yml up -d
```
- **Backend**: `http://localhost:8082`
- **Frontend**: `http://localhost:8081`

### 1.2 개발 모드 (Development Mode)
소스 코드 변경 사항을 실시간으로 확인하며 개발하려면 개발용 Compose 파일을 사용하세요.

1.  **개발 환경 실행**:
    ```bash
    # 호스트 소스 코드 연결 (Volume Mount) 및 Hot Reload 활성화
    docker-compose -f docker-compose.dev.yml up -d
    ```
2.  **동작 방식**:
    - **Backend**: `air`를 통해 Go 코드가 변경될 때마다 자동으로 재빌드/재시작됩니다.
    - **Frontend**: Vite Dev Server가 실행되어 즉각적인 HMR(Hot Module Replacement)을 지원합니다.
    - **접속**: Frontend(`http://localhost:8081`), Backend API(`http://localhost:8082`)

3.  **개발 시 테스트 방법**:
    - API 수정 후: `curl`로 즉시 테스트 (서버 자동 재시작됨).
    - UI 수정 후: 브라우저 새로고침 없이 즉시 반영 확인.
    - 로그 확인: `docker-compose -f docker-compose.dev.yml logs -f`

### 1.3 배포용 릴리즈 (Release Build)
개발이 완료되면 프로덕션용 이미지를 빌드하고 배포합니다.

1.  **프로덕션 빌드 및 실행**:
    ```bash
    # 최신 코드로 빌드 및 컨테이너 교체
    docker-compose -f docker-compose.prod.yml up -d --build
    ```
2.  **오프라인 배포 (Offline Deployment)**:
    - 인터넷이 없는 환경으로 이관 시 사용합니다.
    ```bash
    ./offline-save.sh  # 현재 빌드된 이미지 추출
    # ... 파일 이관 후 ...
    ./offline-load.sh  # 대상 서버에서 로드 및 실행
    ```

### 1.4 Python 스케줄러 (Data Downloader)
데이터 수집을 위한 별도의 Python 컨테이너가 추가되었습니다.

1.  **구성 요소**: Python 3.13, Pandas, DuckDB, Schedule
2.  **설정 방법**:
    `python-scheduler/.env` 파일을 생성하여 DB 접속 정보를 설정하세요.
    ```bash
    cp python-scheduler/.env.example python-scheduler/.env
    vi python-scheduler/.env
    ```
3.  **실행**: `docker-compose.dev.yml`에 포함되어 자동으로 실행됩니다.
    *   개별 실행: `docker-compose -f docker-compose.dev.yml up -d python-scheduler`

> **Tip: Python 스케줄러만 업데이트하기**
>
> 전체 이미지를 다시 옮길 필요 없이 Python 이미지만 따로 생성하여 이동할 수 있습니다.
> 1. (온라인) `./offline-save.sh python` 실행 -> `python-scheduler.tar` 생성.
> 2. (오프라인) 파일 복사 후 `./offline-load.sh` 실행.
> 3. (오프라인) `docker-compose -f docker-compose.dev.yml up -d --no-deps python-scheduler` 로 재시작.

---

## 2. API 리스트

LGD liteStat은 RESTful API를 통해 데이터 조회 및 분석 기능을 제공합니다. 

### 주요 API 요약

| URL | Method | 설명 |
|-----|--------|------|
| `/api/ingest` | POST | 데이터 수집 및 적재 (Backfill) |
| `/api/mart/refresh` | POST | 데이터 마트(통계 뷰) 갱신 |
| `/api/analyze` | POST | 불량 분석 요청 |
| `/api/equipment/rankings` | GET | 장비 불량 랭킹 조회 |
| `/api/config` | GET/PUT | 시스템 설정 관리 |

> 📖 **상세 레퍼런스**
>
> 모든 API의 파라미터와 상세 예제는 **[API.md](./API.md)** 문서에서 확인하실 수 있습니다.

---

## 3. Backfill (과거 데이터 다운로드) 방법

시스템 초기 구축 시나 누락된 과거 데이터를 채워 넣기 위해 Backfill 작업을 수행합니다.

### 단계 1: 데이터 수집 요청 (`/api/ingest`)

`POST /api/ingest` 엔드포인트를 사용하여 특정 기간의 데이터를 수집(다운로드)합니다. 이 작업은 백그라운드에서 수행됩니다.

**예제: 2024년 1월 데이터 다운로드**
```bash
curl -X POST http://localhost:8082/api/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "start_time": "2024-01-01T00:00:00Z",
    "end_time": "2024-01-31T23:59:59Z"
  }'
```

### 단계 2: 데이터 마트 갱신 (`/api/mart/refresh`)

수집된 Raw Data를 기반으로 통계 분석을 위한 Materialized View를 갱신해야 분석 결과에 반영됩니다. 대량의 데이터 수집 후에는 반드시 이 API를 호출해주세요.

```bash
curl -X POST http://localhost:8082/api/mart/refresh
```

### 팁 (Tip)
- 데이터 양이 많을 경우 기간을 나누어(예: 일 단위 또는 주 단위) 요청하는 것을 권장합니다.
- 수집 상태나 로그는 서버 로그를 통해 확인할 수 있습니다.
