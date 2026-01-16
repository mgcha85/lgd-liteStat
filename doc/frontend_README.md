# LGD liteStat 프론트엔드

Svelte 5 + Vite 기반의 모던 웹 애플리케이션입니다.

## 🛠️ 기술 스택

| 기술 | 버전 | 용도 |
|-----|------|------|
| **Svelte** | 5.x | 리액티브 UI 프레임워크 |
| **Vite** | 7.x | 빌드 도구 및 개발 서버 |
| **DaisyUI** | 4.x | Tailwind 기반 컴포넌트 라이브러리 |
| **Plotly.js** | - | 차트 및 히트맵 시각화 |

---

## 📁 프로젝트 구조

```
frontend/
├── src/
│   ├── lib/
│   │   ├── Dashboard.svelte    # 메인 대시보드 컴포넌트
│   │   ├── Settings.svelte     # 설정 페이지 (IBM Carbon 스타일)
│   │   ├── api.js              # API 호출 유틸리티
│   │   └── store.js            # Svelte Store (전역 상태)
│   ├── App.svelte              # 라우팅 및 레이아웃
│   └── main.js                 # 진입점
├── index.html
├── vite.config.js
└── package.json
```

---

## 🚀 로컬 개발

```bash
# 의존성 설치
npm install

# 개발 서버 실행 (HMR 지원)
npm run dev
```

개발 서버는 기본적으로 `http://localhost:5173`에서 실행됩니다.

---

## 🏗️ 프로덕션 빌드

```bash
# 정적 파일 빌드
npm run build
```

빌드된 파일은 `dist/` 폴더에 생성됩니다. 프로덕션 배포 시 Docker/Podman을 통해 Nginx 컨테이너로 서빙됩니다.

---

## 🌏 UI 특징

### 한글화
- 모든 UI 텍스트가 한글로 제공됩니다.
- 탭: 📊 대시보드, ⚙️ 설정
- 버튼, 라벨, 알림 메시지 등 전체 한글화 완료

### IBM Carbon Design (설정 페이지)
- 하단 테두리(Bottom Border) 스타일의 인풋 필드
- 태그 형식의 불량 용어 표시
- 인라인 알림 스타일의 저장 결과 메시지

### 테마
- 다크/라이트 모드 지원
- DaisyUI 테마 컨트롤러 사용

---

## 🔧 주요 컴포넌트

### Dashboard.svelte
- 공장(Facility) 선택
- 날짜 범위 선택 (기본: 최근 7일)
- 불량 유형 필터
- 장비 랭킹 테이블 (Best/Worst 10)
- 산점도, 히트맵, 일별 추이 차트

### Settings.svelte
- 분석 설정 (Top N 제한, 기본 불량 유형)
- 데이터 보존 기간
- 스케줄러 설정

---

## 📝 IDE 설정

권장 IDE: [VS Code](https://code.visualstudio.com/) + [Svelte 확장](https://marketplace.visualstudio.com/items?itemName=svelte.svelte-vscode)

`checkJs` 옵션이 활성화되어 있어 JavaScript에서도 타입 체크를 지원합니다.
