# LGD liteStat 분석 알고리즘 문서

## 1. 장비 계층 구조 (Equipment Hierarchy)

LGD 공정에서 장비는 다음과 같은 계층 구조를 가집니다:

```
process_code (공정)                    ← Parent (최상위)
  └─ equipment_group_id (장비 그룹)   ← 예: CVD, ETCH
       └─ equipment_line_id (라인)    ← 예: CVD01, CVD02
            └─ equipment_machine_id   ← 예: CVD0101, CVD0201
                 └─ equipment_unit_id ← 예: CVD010101 (최하위)
```

### 예시
| Level | 예시 값 | 설명 |
|-------|--------|------|
| Process Code | `DEPO` | CVD 증착 공정 |
| Equipment Group | `CVD` | CVD 장비군 |
| Equipment Line | `CVD01` | CVD 1호 라인 |
| Equipment Machine | `CVD0101` | 1호 라인의 1번 머신 |
| Equipment Unit | `CVD010101` | 1번 머신의 1번 유닛 |

---

## 2. 분석 레벨 (Analysis Levels)

### 2.1 Line-Level 분석 (장비 랭킹)

**목적**: 장비 라인별 불량률 비교 및 랭킹 산출

**GROUP BY 조건**:
```sql
GROUP BY h.process_code, i.equipment_group_id, h.equipment_line_id, h.product_type_code
```

**핵심 로직**:
1. `process_code`: 동일 공정 내에서만 비교
2. `equipment_group_id`: 동일 장비 그룹 내에서만 비교
3. `equipment_line_id`: 라인 단위로 집계
4. `product_type_code`: 모델별 분리 집계

### 2.2 Glass-Level 분석
- `product_id`, `lot_id`, `work_date`별 집계
- 개별 Glass 단위 불량 추적

### 2.3 Lot-Level 분석
- `lot_id`, `group_type`별 집계
- Lot 단위 품질 통계

### 2.4 Daily-Level 분석
- `work_date`, `group_type`별 집계
- 일자별 트렌드 분석

---

## 3. Product ID 중복 처리

### 문제
동일한 `product_id`가 여러 공정/장비를 거쳐 **중복 레코드**로 존재할 수 있음.

### 해결 방법

```sql
WITH deduplicated_history AS (
    SELECT *, 
           ROW_NUMBER() OVER (
               PARTITION BY product_id, process_code, equipment_line_id
               ORDER BY move_in_ymdhms DESC  -- 가장 최근 레코드 우선
           ) as rn
    FROM lake_mgr.mas_pnl_prod_eqp_h
),
filtered_history AS (
    SELECT * FROM deduplicated_history WHERE rn = 1  -- 최신 1건만 유지
)
```

**규칙**:
- 동일 `product_id` + `process_code` + `equipment_line_id` 조합에서
- `move_in_ymdhms` 기준 **가장 마지막(최신) 레코드만 유지**
- 나머지 중복 레코드는 제거

---

## 4. Equipment Group ID 획득

### 추출 방식
- `equipment_group_id`는 `equipment_line_id`에서 **직접 추출**
- Python 기준: `equipment_line_id[2:6]`
- SQL 기준: `SUBSTRING(equipment_line_id, 3, 4)`

### 예시
| equipment_line_id | equipment_group_id |
|-------------------|-------------------|
| `A1CVD01` | `CVD0` |
| `B2ETCH03` | `ETCH` |

```sql
SUBSTRING(h.equipment_line_id, 3, 4) as equipment_group_id
```

---

## 5. 불량률 계산 공식

### Delta 공식
```
Delta = Others_Avg - Overall_Avg
```

- **Others_Avg**: 해당 장비를 제외한 나머지 장비들의 평균 불량률
- **Overall_Avg**: 전체 장비 평균 불량률
- **Delta < 0**: 해당 장비가 평균보다 불량이 많음 (문제 장비)
- **Delta > 0**: 해당 장비가 평균보다 불량이 적음 (양호)

### 최소 샘플 기준
- `product_count >= 10`: 최소 10개 이상 제품을 처리한 장비만 랭킹에 포함
