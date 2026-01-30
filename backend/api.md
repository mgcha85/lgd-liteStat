# Backend API Documentation

이 문서는 `lgd-liteStat` 백엔드 서버의 주요 API 엔드포인트와 사용법을 `curl` 명령어 예시와 함께 설명합니다.

---

## 1. 계층적 분석 (Hierarchy Analysis) - **New**

최근 추가된 기능으로, 설비 정보(Process > Line > Machine > Path)를 기반으로 불량 통계를 계층적으로 집계합니다. `glass_stats`와 `history`(Parquet)를 조인하여 분석합니다.

*   **URL**: `POST /api/analyze/hierarchy`
*   **설명**: 사용자가 지정한 레벨(`analysis_level`)에 따라 그룹핑하여 통계(불량 수, DPU, 불량 맵 등)를 반환합니다.

### 요청 파라미터 (JSON)
*   `facility` (필수): 설비 코드 (예: "A1")
*   `start`, `end`: 분석 기간 (YYYY-MM-DD)
*   `model_code`: 모델 코드 (필수)
*   `defect_name`: (옵션) 특정 불량만 필터링
*   `analysis_level`: 집계 레벨 (`process`, `line`, `machine`, `path`)

### Curl 예시
```bash
# 1. Machine 레벨로 설비별 불량 분석
curl -X POST http://localhost:8082/api/analyze/hierarchy \
  -H "Content-Type: application/json" \
  -d '{
    "facility": "A1",
    "start": "2023-10-01",
    "end": "2023-10-02",
    "model_code": "M1",
    "analysis_level": "machine"
  }'

# 2. 특정 공정(Process) 내의 Path 레벨 분석
curl -X POST http://localhost:8082/api/analyze/hierarchy \
  -H "Content-Type: application/json" \
  -d '{
    "facility": "A1",
    "start": "2023-10-01",
    "end": "2023-10-01",
    "model_code": "M1",
    "process_code": "P100",
    "analysis_level": "path"
  }'
```

---

## 2. 배치 분석 (Batch Analysis)

특정 날짜의 데이터를 배치로 분석하여 요약 정보를 생성합니다. (기존 기능)

*   **URL**: `POST /api/analyze/batch`
*   **설명**: 특정 날짜의 검사 데이터를 분석하여 `glass_stats` 테이블에 저장하거나 결과를 반환합니다.

### Curl 예시
```bash
curl -X POST http://localhost:8082/api/analyze/batch \
  -H "Content-Type: application/json" \
  -d '{
    "facility": "A1",
    "date": "2023-10-01"
  }'
```

---

## 3. 원본 데이터 조회 (Raw Data)

### 3.1 검사 데이터 조회
*   **URL**: `GET /api/inspection`
*   **설명**: 수집된 검사(Inspection) 파케이 데이터의 Raw Data를 조회합니다.

```bash
curl "http://localhost:8082/api/inspection?facility=A1&date=2023-10-01&limit=5"
```

### 3.2 진행 이력 조회
*   **URL**: `GET /api/history`
*   **설명**: 수집된 진행 이력(History) 데이터(설비 정보 포함)를 조회합니다.

```bash
curl "http://localhost:8082/api/history?facility=A1&date=2023-10-01&model=M1&limit=5"
```

---

## 4. 설정 관리 (Configuration)

### 4.1 전체 설정 조회
*   **URL**: `GET /api/config`
*   **설명**: 현재 서버에 로드된 `config.yaml` 설정을 조회합니다.

```bash
curl http://localhost:8082/api/config
```

### 4.2 스케줄러 설정 변경
*   **URL**: `PUT /api/config/scheduler`
*   **설명**: 자동 분석 스케줄(Cron)을 변경합니다.

```bash
curl -X PUT http://localhost:8082/api/config/scheduler \
  -H "Content-Type: application/json" \
  -d '{
    "cron": "0 2 * * *", 
    "enabled": true
  }'
```

---

## 5. 대시보드 및 리포트

### 5.1 설비 랭킹 (Equipment Rankings)
*   **URL**: `GET /api/equipment/rankings`
*   **설명**: 불량률이 높은 설비 순위를 반환합니다.

```bash
curl "http://localhost:8082/api/equipment/rankings?facility=A1&date=2023-10-01"
```

### 5.2 글래스 상세 분석
*   **URL**: `GET /api/analyze/glass/{glass_id}`
*   **설명**: 특정 Glass ID의 상세 불량 정보를 조회합니다.

```bash
curl http://localhost:8082/api/analyze/glass/GLS-12345
```
