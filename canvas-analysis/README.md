# LGD Canvas Analysis Project

## 프로젝트 개요
디스플레이 공정에서 원장 Glass 내의 Panel 불량 정보를 분석하기 위한 프로젝트입니다. 
원장 Glass는 여러 개의 Panel로 쪼개지며, 모델별로 Panel의 개수는 다르지만 원장은 동일하므로 모델 구분 없이 원장 기준으로 분석을 수행합니다.
Panel 사이의 Gap을 제거하고, 사용자가 정의한 NxM Grid로 재구성하여 분석할 수 있는 기능을 제공합니다.

### 주요 기능
- **결함 데이터 전처리**: 날짜/숫자 변환 및 Panel 주소 생성 (Product ID 제거)
- **Gap 제거 (Center Aligned)**: 
    - Panel 사이의 물리적 간격을 제거하여 논리적 좌표계로 변환합니다.
    - Gap 제거 후 전체 영역의 중심을 원점(0,0)으로 재정렬하여 좌표 왜곡을 방지합니다.
- **Grid 재구성**: 
    - 사용자가 지정한 NxM 크기의 Grid로 영역을 나눕니다.
    - **Sub Panel Address 생성**: x, y라벨 모두 1~z (대문자 미포함), 방향은 둘 다 좌하단에서 우상단 가는 방향으로 숫자가 커짐, x+y순서로 sub_panel_addr 생성 (예: `1k`, `af`)
- **API 제공**: FastAPI를 통해 데이터 업로드 및 분석 결과(Parquet) 다운로드 제공
- **시각화**: 검증을 위한 산점도(Scatter Plot) 및 히트맵(Heatmap) 생성 스크립트 포함

## 기술 스택
- **Language**: Python 3.11+
- **Web Framework**: FastAPI
- **Data Processing**: Polars (High-performance DataFrame library)
- **Package Manager**: uv
- **Visualization**: Matplotlib, Seaborn

## 프로젝트 구조
```
lgd-canvas-analysis/
├── config.yaml              # 기본 설정 파일 (Grid 크기, Glass 크기 등)
├── logic.py                 # 핵심 로직 (전처리, Gap 제거, Grid 계산)
├── main.py                  # FastAPI 어플리케이션 및 엔드포인트
├── mock_data_generator.py   # 테스트용 Mock 데이터 생성 스크립트
├── visualize.py             # 결과 검증을 위한 시각화 스크립트
├── defect.parquet           # (Generated) 결함 데이터
├── pnl_map.parquet          # (Generated) 패널 맵 데이터
└── README.md                # 프로젝트 문서
```

## 설치 및 실행 방법

### 1. 환경 설정 및 의존성 설치
본 프로젝트는 `uv`를 패키지 매니저로 사용합니다.

```bash
# uv 설치 (없는 경우)
curl -LsSf https://astral.sh/uv/install.sh | sh

# 의존성 설치 및 가상환경 설정
uv sync
```

### 2. Mock 데이터 생성
테스트를 위한 가상의 결함 데이터와 패널 맵 데이터를 생성합니다.
(기본설정: 5x4 패널 배치, User Convention: 가로=A,B.. / 세로=1,2..)

```bash
uv run python mock_data_generator.py
```
실행 후 `defect.parquet`와 `pnl_map.parquet` 파일이 생성됩니다.

### 3. 서버 실행
FastAPI 서버를 실행합니다.

```bash
uv run uvicorn main:app --reload
```
서버는 기본적으로 `http://127.0.0.1:8000`에서 실행됩니다.
API 문서는 `http://127.0.0.1:8000/docs`에서 확인할 수 있습니다.

### 4. 시각화 실행 (검증)
로직 검증을 위해 산점도와 히트맵을 생성합니다.
좌측은 원본 데이터, 우측은 Gap 제거 및 Grid가 적용된 데이터를 시각화합니다.

```bash
uv run python visualize.py
```
실행 후 `scatter_plot.png`와 `heatmap.png` 이미지가 생성됩니다.

## 설정 (config.yaml)
`config.yaml` 파일에서 기본 Grid 크기(N, M) 및 Glass 크기를 설정할 수 있습니다.

```yaml
grid:
  N: 10
  M: 20
glass:
  width: 1500
  height: 1800
```
