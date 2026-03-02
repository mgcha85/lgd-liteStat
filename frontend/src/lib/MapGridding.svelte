<script>
    import { analyzeMapPattern, getProductsFromPattern } from "./api.js";
    import { activeTab, routeAnalysisProducts } from "./store.js";

    let defectFile = null;
    let facilityCode = "A1T";
    let partNoName = "";
    let gridN = 10;
    let gridM = 20;
    let loading = false;
    let patternLoading = false;
    let error = null;
    let success = null;

    // Pattern Analysis State
    let rankedPatterns = [];
    let patternDownloadUrl = null;

    function handleFileChange(event) {
        defectFile = event.target.files[0];
    }

    async function processData() {
        if (!defectFile || !facilityCode || !partNoName) {
            error = "모든 필수 항목을 입력해주세요.";
            return;
        }

        loading = true;
        error = null;
        success = null;

        const formData = new FormData();
        formData.append("defect_file", defectFile);
        formData.append("facility_code", facilityCode);
        formData.append("part_no_name", partNoName);
        formData.append("N", String(gridN));
        formData.append("M", String(gridM));

        try {
            const response = await fetch("/canvas/analyze/grid", {
                method: "POST",
                body: formData,
            });

            if (!response.ok) {
                let errText = await response.text();
                throw new Error(
                    `처리 실패: ${response.status} ${response.statusText}\n${errText}`,
                );
            }

            const blob = await response.blob();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = `processed_${facilityCode}_${partNoName}.parquet`;
            document.body.appendChild(a);
            a.click();
            a.remove();
            window.URL.revokeObjectURL(url);
            success = "그리딩 처리 완료! 파일이 다운로드됩니다.";
        } catch (e) {
            error = e.message;
        } finally {
            loading = false;
        }
    }

    async function runPatternAnalysis() {
        if (!defectFile || !facilityCode || !partNoName) {
            error = "모든 필수 항목을 입력해주세요.";
            return;
        }

        if (gridN === 0 || gridM === 0) {
            error =
                "패턴 분석을 위해서는 N과 M(그리드 분할 수)이 0보다 커야 합니다. (기본값: 10 x 20)";
            return;
        }

        patternLoading = true;
        error = null;
        success = null;
        rankedPatterns = [];
        patternDownloadUrl = null;

        try {
            const result = await analyzeMapPattern(
                defectFile,
                facilityCode,
                partNoName,
                gridN,
                gridM,
            );
            rankedPatterns = result.ranked_patterns || [];
            if (result.download_url) {
                patternDownloadUrl = import.meta.env.VITE_MAP_PATTERN_API_URL
                    ? `${import.meta.env.VITE_MAP_PATTERN_API_URL}${result.download_url}`
                    : `http://localhost:8003${result.download_url}`;
            }
            success = "맵 패턴 분석이 완료되었습니다. 아래 결과를 확인하세요.";
        } catch (e) {
            error = e.message;
        } finally {
            patternLoading = false;
        }
    }

    async function extractAndRoute(panels) {
        if (!panels || panels.length === 0) return;

        patternLoading = true;
        error = null;
        try {
            const result = await getProductsFromPattern(
                defectFile,
                facilityCode,
                partNoName,
                gridN,
                gridM,
                panels,
            );

            const pIds = result.product_ids || [];
            if (pIds.length === 0) {
                alert(
                    "해당 패턴 영역에 위치한 불량을 가진 Product가 없습니다.",
                );
                return;
            }

            // 1. Store the exact product list in global state
            routeAnalysisProducts.set(pIds);

            // 2. Switch tab to Dashboard (Route Analysis)
            activeTab.set("dashboard");
        } catch (e) {
            error = "라우트 분석을 위한 Product ID 추출 실패: " + e.message;
        } finally {
            patternLoading = false;
        }
    }
</script>

<div class="card bg-base-100 shadow-xl max-w-4xl mx-auto mt-6">
    <div class="card-body">
        <h2 class="card-title text-2xl mb-2">
            🧩 맵그리딩 & 패턴 분석 (Map Pattern Analysis)
        </h2>
        <p class="text-gray-500 mb-6">
            Defect 파일에 새로운 Panel Address 그리드를 부여하거나, Geographic
            Pattern(Line, Donut 등)을 분석하고 랭킹을 매깁니다.
        </p>

        {#if error}
            <div class="alert alert-error shadow-lg mb-4">
                <span>{error}</span>
            </div>
        {/if}

        {#if success}
            <div class="alert alert-success shadow-lg mb-4">
                <span>{success}</span>
            </div>
        {/if}

        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <!-- Left Column: Inputs -->
            <div>
                <div class="form-control w-full mb-4">
                    <label class="label" for="defect-file">
                        <span class="label-text font-bold"
                            >Defect Parquet File</span
                        >
                    </label>
                    <input
                        id="defect-file"
                        type="file"
                        accept=".parquet"
                        class="file-input file-input-bordered w-full"
                        on:change={handleFileChange}
                    />
                </div>

                <div class="form-control w-full mb-4">
                    <label class="label" for="facility-code">
                        <span class="label-text font-bold">Facility Code</span>
                    </label>
                    <input
                        id="facility-code"
                        type="text"
                        placeholder="예: A1T"
                        bind:value={facilityCode}
                        class="input input-bordered w-full"
                    />
                </div>

                <div class="form-control w-full mb-4">
                    <label class="label" for="part-no-name">
                        <span class="label-text font-bold"
                            >Part No Name / Model Code</span
                        >
                    </label>
                    <input
                        id="part-no-name"
                        type="text"
                        placeholder="예: MODEL123"
                        bind:value={partNoName}
                        class="input input-bordered w-full"
                    />
                </div>

                <!-- Grid Size Row -->
                <div class="flex gap-4 mb-2">
                    <div class="form-control flex-1">
                        <label class="label" for="grid-n">
                            <span class="label-text font-bold"
                                >N (열 분할 수)</span
                            >
                            <span class="label-text-alt text-gray-400"
                                >기본: 10</span
                            >
                        </label>
                        <input
                            id="grid-n"
                            type="number"
                            min="0"
                            placeholder="10"
                            bind:value={gridN}
                            class="input input-bordered w-full"
                        />
                    </div>
                    <div class="form-control flex-1">
                        <label class="label" for="grid-m">
                            <span class="label-text font-bold"
                                >M (행 분할 수)</span
                            >
                            <span class="label-text-alt text-gray-400"
                                >기본: 20</span
                            >
                        </label>
                        <input
                            id="grid-m"
                            type="number"
                            min="0"
                            placeholder="20"
                            bind:value={gridM}
                            class="input input-bordered w-full"
                        />
                    </div>
                </div>

                {#if gridN === 0 || gridM === 0}
                    <div class="alert alert-info shadow-sm mb-4 py-2">
                        <span class="text-sm"
                            >💡 N=0 또는 M=0 이면 단순 그리딩만 가능하며 패턴
                            분석은 작동하지 않습니다.</span
                        >
                    </div>
                {/if}

                <div class="card-actions justify-end mt-4 border-t pt-4">
                    <button
                        class="btn btn-outline"
                        on:click={processData}
                        disabled={loading || patternLoading}
                    >
                        {#if loading}
                            <span class="loading loading-spinner"></span>
                        {/if}
                        단순 그리딩 다운로드
                    </button>

                    <button
                        class="btn btn-primary ml-2"
                        on:click={runPatternAnalysis}
                        disabled={loading || patternLoading}
                    >
                        {#if patternLoading}
                            <span class="loading loading-spinner"></span>
                        {/if}
                        맵 패턴 분석 실행 🚀
                    </button>
                </div>
            </div>

            <!-- Right Column: Results -->
            <div
                class="bg-base-200 rounded-xl p-4 border border-base-300 overflow-y-auto"
                style="max-height: 500px"
            >
                <h3 class="font-bold text-lg mb-4 border-b pb-2">
                    🏆 패턴 분류 & 랭킹 결과
                </h3>

                {#if rankedPatterns.length === 0 && !patternLoading}
                    <div class="text-center text-gray-400 py-10">
                        <p>
                            분석을 실행하면 여기에 감지된 패턴과 랭킹이
                            표시됩니다.
                        </p>
                        <p class="text-xs mt-2">
                            (Line > Donut > Cluster 우선순위)
                        </p>
                    </div>
                {:else if patternLoading}
                    <div
                        class="flex flex-col items-center justify-center py-10 text-primary"
                    >
                        <span class="loading loading-bars loading-lg mb-4"
                        ></span>
                        <p>AI 패턴 디텍터 연산 중...</p>
                    </div>
                {:else}
                    <div class="flex flex-col gap-3">
                        {#if patternDownloadUrl}
                            <a
                                href={patternDownloadUrl}
                                class="btn btn-sm btn-success mb-2"
                                target="_blank"
                            >
                                📥 패턴 태깅된 Parquet 다운로드
                            </a>
                        {/if}

                        {#each rankedPatterns as pattern}
                            <button
                                type="button"
                                class="card text-left bg-base-100 shadow-sm border border-gray-200 cursor-pointer hover:border-primary transition-colors focus:outline-none focus:ring-2 focus:ring-primary w-full"
                                on:click={() => extractAndRoute(pattern.panels)}
                            >
                                <div class="card-body p-4 w-full">
                                    <div
                                        class="flex justify-between items-start"
                                    >
                                        <div class="flex items-center gap-2">
                                            <div
                                                class="badge badge-primary font-bold"
                                            >
                                                Rank {pattern.rank}
                                            </div>
                                            <h4
                                                class="font-bold text-lg uppercase"
                                            >
                                                {pattern.type}
                                            </h4>
                                        </div>
                                        <div class="badge badge-outline">
                                            {pattern.panels.length} Panels
                                        </div>
                                    </div>
                                    <p
                                        class="text-xs text-primary mt-2 flex items-center gap-1 font-medium"
                                    >
                                        🚀 클릭하여 해당 패턴의 <strong
                                            >{pattern.panels.length}</strong
                                        >개 패널 기준
                                        <strong>Route Analysis</strong> 즉시 실행
                                    </p>
                                </div>
                            </button>
                        {/each}
                    </div>
                {/if}
            </div>
        </div>
    </div>
</div>
