# API 명세서

## 개요
LGD Canvas Analysis 프로젝트의 API 명세입니다.
Base URL: `http://127.0.0.1:8000`

## Endpoints

### 1. 결함 분석 (Analyze Defects)

업로드된 결함 데이터와 패널 맵 데이터를 사용하여 Gap을 제거하고 Grid를 적용한 분석 결과를 반환합니다.

- **URL**: `/analyze`
- **Method**: `POST`
- **Content-Type**: `multipart/form-data`

#### 요청 파라미터 (Form Data)

| 파라미터명 | 타입 | 필수 여부 | 설명 |
|---|---|---|---|
| `defect_file` | File | Required | 결함 정보가 담긴 Parquet 파일 (`defect.parquet`) |
| `pnl_map_file` | File | Required | 패널 좌표 정보가 담긴 Parquet 파일 (`pnl_map.parquet`) |
| `facility_code` | String | Required | 설비 코드 (데이터 필터링용) |
| `part_no_name` | String | Required | 모델명 (데이터 필터링용) |
| `N` | Integer | Optional | Grid 가로 분할 개수 (기본값: config.yaml 참조) |
| `M` | Integer | Optional | Grid 세로 분할 개수 (기본값: config.yaml 참조) |

#### 처리 로직 상세
1. **전처리**: 결함 데이터의 좌표, 사이즈를 숫자형으로 변환하고 날짜 포맷을 통일합니다. Panel ID에서 Product ID를 제거하여 `panel_addr`를 생성합니다.
2. **Gap 제거**: `pnl_map` 데이터를 기반으로 각 Panel 사이의 Gap을 계산하여 제거합니다. 이때 전체 영역의 중심을 (0,0)으로 맞추어(Center Aligned) 좌표를 재조정합니다.
3. **Grid 적용**: Gap이 제거된 전체 영역을 NxM Grid로 나눕니다.
4. **주소 생성**: 각 Grid Cell에 대해 `sub_panel_addr`를 부여합니다.
    - Label 순서: `1, 2, ..., 9, a, b, ..., z`
    - 형식: `{가로Label}{세로Label}` (예: `1a`, `9z`)

#### 응답 (Response)

- **Status Code**: `200 OK`
- **Content-Type**: `application/octet-stream`
- **Body**: 처리된 Parquet 파일 (`processed_defect.parquet`)

처리된 파일에는 원본 컬럼 외에 다음 컬럼들이 추가/변환되어 포함됩니다:

| 컬럼명 | 설명 |
|---|---|
| `panel_addr` | `panel_id`에서 추출한 패널 주소 (예: `A1`) |
| `gapless_x` | Gap이 제거되고 중심 정렬된 X 좌표 |
| `gapless_y` | Gap이 제거되고 중심 정렬된 Y 좌표 |
| `grid_col_idx` | Grid 열 인덱스 (0-based) |
| `grid_row_idx` | Grid 행 인덱스 (0-based) |
| `sub_panel_addr` | 생성된 Sub Panel 주소 (예: `1a`) |

#### 에러 응답

- **400 Bad Request**: 
    - 패널 맵 데이터가 조회되지 않는 경우 (`No matching panel map data found.`)
- **500 Internal Server Error**: 
    - 서버 내부 처리 중 오류 발생 시

#### 호출 예시 (cURL)

```bash
curl -X 'POST' \
  'http://127.0.0.1:8000/analyze' \
  -H 'accept: application/json' \
  -H 'Content-Type: multipart/form-data' \
  -F 'defect_file=@defect.parquet' \
  -F 'pnl_map_file=@pnl_map.parquet' \
  -F 'facility_code=FAC1' \
  -F 'part_no_name=MODEL_A' \
  -F 'N=10' \
  -F 'M=20' \
  --output processed_result.parquet
```
