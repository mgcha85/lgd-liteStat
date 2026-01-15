<script>
    import { onMount } from "svelte";
    import {
        getEquipmentRankings,
        requestAnalysis,
        getAnalysisStatus,
        getAnalysisResults,
        triggerIngest,
        refreshMart,
        analyzeBatch,
        streamBatchAnalysis,
        getAnalysisLogs,
        getConfig,
    } from "./api.js";
    import AnalysisCard from "./AnalysisCard.svelte";

    import Plotly from "plotly.js-dist-min";

    export let config;

    let loading = false;
    let error = null;
    let rankings = [];
    let filteredRankings = [];
    let selectedEquipment = null;
    let analysisResults = null;
    let jobStatus = null;

    let batchResults = {}; // Map<EquipmentID, Results>
    let batchLoading = false;
    let batchError = null;
    let batchAttempted = false;

    // Pagination
    let currentPage = 1;
    let pageSize = 20;

    $: paginatedRankings = filteredRankings.slice(
        (currentPage - 1) * pageSize,
        currentPage * pageSize,
    );
    $: totalPages = Math.ceil(filteredRankings.length / pageSize);

    // Reset pagination when filter results change
    let previousFilteredLength = 0;
    $: if (filteredRankings.length !== previousFilteredLength) {
        currentPage = 1;
        previousFilteredLength = filteredRankings.length;
    }

    function changePage(newPage) {
        if (newPage >= 1 && newPage <= totalPages) {
            currentPage = newPage;
        }
    }

    // ... (downloadExcel function remains same) ...
    function downloadExcel() {
        if (!filteredRankings || filteredRankings.length === 0) return;

        // Create CSV Header
        const headers = [
            "순위",
            "설비 ID",
            "공정",
            "불량률",
            "차이",
            "총 불량 수",
            "글라스 수",
        ];

        // Map Data
        const rows = filteredRankings.map((r, index) => [
            index + 1,
            r.equipment_id,
            r.process_code,
            (r.defect_rate * 100).toFixed(4) + "%",
            (r.delta * 100).toFixed(4) + "%",
            r.total_defects,
            r.glass_count,
        ]);

        const csvContent = [
            headers.join(","),
            ...rows.map((row) => row.join(",")),
        ].join("\n");

        const blob = new Blob([csvContent], {
            type: "text/csv;charset=utf-8;",
        });
        const url = URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.setAttribute("href", url);
        link.setAttribute(
            "download",
            `analysis_rankings_${startDate}_${endDate}.csv`,
        );
        link.style.visibility = "hidden";
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    }

    let activeTab = "dashboard";
    // performanceLogs removed

    // Filters
    // Default to 2 weeks ago
    let today = new Date();
    let twoWeeksAgo = new Date(today.getTime() - 14 * 24 * 60 * 60 * 1000);
    let startDate = twoWeeksAgo.toISOString().split("T")[0];
    let endDate = today.toISOString().split("T")[0];

    // ... variables ...
    let defectTerms = config?.Settings?.DefectTerms || [];
    let defectName = defectTerms[0] || "";
    let processCodeFilter = "";
    let equipmentFilter = "";

    // Chart containers
    let glassChartDiv;
    let lotChartDiv;
    let dailyChartDiv;
    let heatmapChartDiv;

    $: if (config && config.Settings?.DefectTerms) {
        defectTerms = config.Settings.DefectTerms;
        if (!defectName && defectTerms.length > 0) defectName = defectTerms[0];
    }

    // Client-side filtering (Pure Reactive)
    $: filteredRankings = getFilteredRankings(
        rankings,
        processCodeFilter,
        equipmentFilter,
    );

    onMount(async () => {
        // await loadRankings(); // Auto-load disabled
    });

    // Reactive Trigger for Batch Analysis
    $: if (
        filteredRankings &&
        filteredRankings.length > 0 &&
        !batchAttempted &&
        !batchLoading &&
        !loading
    ) {
        runStreamingAnalysis(filteredRankings.slice(0, 20));
    }

    function getFilteredRankings(data, pFilter, eFilter) {
        if (!data) return [];
        return data;
        // TEMPORARY: Bypass filters to force rendering
        // logic commented out for now
    }

    const BATCH_CHUNK_SIZE = 5;
    let processedCount = 0;
    let totalTargets = 0;

    async function runStreamingAnalysis(targets) {
        if (targets.length === 0) return;

        batchLoading = true;
        batchError = null;
        processedCount = 0;
        totalTargets = targets.length;

        if (!batchAttempted) {
            batchResults = {};
        }

        const req = {
            defect_name: defectName,
            start_date: startDate,
            end_date: endDate,
            targets: targets.map((t) => ({
                equipment_id: t.equipment_id,
                process_code: t.process_code,
            })),
        };

        try {
            await streamBatchAnalysis(req, (data) => {
                if (data.error) {
                    console.error(
                        `Stream error for ${data.equipment_id}:`,
                        data.error,
                    );
                } else if (data.result) {
                    batchResults[data.equipment_id] = data.result;
                    batchResults = batchResults; // Trigger Reflow
                    processedCount++;
                }
            });
        } catch (e) {
            console.error("Streaming Analysis Fatal Error:", e);
            batchError = e.message || "Unknown error";
        } finally {
            batchLoading = false;
            batchAttempted = true;
        }
    }

    async function analyzeEquipment(equipment) {
        selectedEquipment = equipment;
        loading = true;
        error = null;

        try {
            // Request analysis
            const response = await requestAnalysis({
                defect_name: defectName,
                start_date: startDate,
                end_date: endDate,
                equipment_ids: [equipment.equipment_id],
                process_codes: [equipment.process_code], // Analyze specific process
            });

            const jobId = response.job_id;

            // Poll for completion
            let attempts = 0;
            while (attempts < 30) {
                await new Promise((resolve) => setTimeout(resolve, 2000));
                const status = await getAnalysisStatus(jobId);
                jobStatus = status;

                if (status.status === "completed") {
                    const results = await getAnalysisResults(jobId, 100);
                    analysisResults = results;
                    // Small delay to ensure DOM is ready for charts
                    setTimeout(() => renderCharts(results), 100);
                    break;
                } else if (status.status === "failed") {
                    throw new Error(status.error_message || "Analysis failed");
                }
                attempts++;
            }

            if (attempts >= 30) {
                throw new Error("Analysis timeout");
            }
        } catch (e) {
            error = e.message;
            analysisResults = null;
        } finally {
            loading = false;
        }
    }

    function renderCharts(results) {
        // Glass plot
        if (glassChartDiv && results.glass_results) {
            const target = results.glass_results.filter(
                (r) => r.group_type === "Target",
            );
            const others = results.glass_results.filter(
                (r) => r.group_type === "Others",
            );

            Plotly.newPlot(
                glassChartDiv,
                [
                    {
                        x: target.map((_, i) => i),
                        y: target.map((r) => r.total_defects),
                        mode: "markers",
                        type: "scatter",
                        name: "Target",
                        marker: { color: "red", size: 6 },
                    },
                    {
                        x: others.map((_, i) => i + target.length),
                        y: others.map((r) => r.total_defects),
                        mode: "markers",
                        type: "scatter",
                        name: "Others",
                        marker: { color: "black", size: 6 },
                    },
                ],
                {
                    title: "Glass-Level Defects",
                    height: 400,
                    margin: { t: 40, b: 40, l: 40, r: 20 },
                },
            );
        }

        // Daily
        if (dailyChartDiv && results.daily_results) {
            const target = results.daily_results.filter(
                (r) => r.group_type === "Target",
            );
            const others = results.daily_results.filter(
                (r) => r.group_type === "Others",
            );
            Plotly.newPlot(
                dailyChartDiv,
                [
                    {
                        x: target.map((r) => r.work_date),
                        y: target.map((r) => r.avg_defects),
                        type: "scatter",
                        mode: "lines+markers",
                        name: "Target",
                        line: { color: "red" },
                    },
                    {
                        x: others.map((r) => r.work_date),
                        y: others.map((r) => r.avg_defects),
                        type: "scatter",
                        mode: "lines+markers",
                        name: "Others",
                        line: { color: "black" },
                    },
                ],
                {
                    title: "Daily Trend",
                    height: 400,
                    margin: { t: 40, b: 40, l: 40, r: 20 },
                },
            );
        }

        // Heatmap
        if (heatmapChartDiv && results.heatmap_results?.length > 0) {
            const heatmap = results.heatmap_results;
            const xValues = [...new Set(heatmap.map((h) => h.x))].sort();
            const yValues = [...new Set(heatmap.map((h) => h.y))].sort();
            const zMatrix = yValues.map((y) =>
                xValues.map((x) => {
                    const cell = heatmap.find((h) => h.x === x && h.y === y);
                    return cell ? cell.defect_rate : 0;
                }),
            );
            Plotly.newPlot(
                heatmapChartDiv,
                [
                    {
                        z: zMatrix,
                        x: xValues,
                        y: yValues,
                        type: "heatmap",
                        colorscale: "Reds",
                    },
                ],
                {
                    title: "Panel Map",
                    height: 400,
                    margin: { t: 40, b: 40, l: 40, r: 20 },
                },
            );
        }
    }

    // Toast State
    let toast = null; // { message: "", type: "info|success|error" }

    function showToast(message, type = "info") {
        toast = { message, type };
        setTimeout(() => {
            toast = null;
        }, 3000);
    }

    // Load Rankings
    async function loadRankings() {
        loading = true;
        error = null;
        batchResults = {};
        try {
            const config = await getConfig();
            const topN = config.analysis?.top_n_limit || 20;

            console.log("Querying with defect:", defectName);

            const data = await getEquipmentRankings({
                start_date: startDate,
                end_date: endDate,
                defect_name: defectName,
                limit: 0,
            });
            rankings = data.rankings || [];

            if (rankings.length === 0) {
                showToast("검색 결과가 없습니다.", "info");
            } else {
                showToast(`조회 완료: ${rankings.length}건`, "success");
            }

            batchAttempted = false;
        } catch (e) {
            console.error("Load Rankings Error:", e);
            error = e.message;
            showToast("조회 실패: " + e.message, "error");
        } finally {
            loading = false;
        }
    }

    async function handleIngest() {
        loading = true;
        try {
            const start = new Date(startDate + "T00:00:00Z").toISOString();
            const end = new Date(endDate + "T23:59:59Z").toISOString();
            await triggerIngest(start, end);
            await refreshMart();
            showToast("데이터 수집 및 갱신 완료!", "success");
            await loadRankings();
        } catch (e) {
            showToast("수집 실패: " + e.message, "error");
        } finally {
            loading = false;
        }
    }
</script>

<div class="p-6">
    <!-- Header/Tabs Removed - Single Dashboard View -->
    <div class="mb-6">
        <h1 class="text-2xl font-bold">Display Manufacturing Analysis</h1>
    </div>

    <!-- DASHBOARD CONTENT -->
    <div class="">
        <!-- Controls -->
        <div class="card bg-base-100 shadow-xl mb-6">
            <div class="card-body">
                <div
                    class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4"
                >
                    <label class="form-control w-full">
                        <div class="label">
                            <span class="label-text font-bold">시작일</span>
                        </div>
                        <input
                            type="date"
                            bind:value={startDate}
                            class="input input-bordered w-full"
                        />
                    </label>
                    <label class="form-control w-full">
                        <div class="label">
                            <span class="label-text font-bold">종료일</span>
                        </div>
                        <input
                            type="date"
                            bind:value={endDate}
                            class="input input-bordered w-full"
                        />
                    </label>
                    <label class="form-control w-full">
                        <div class="label">
                            <span class="label-text font-bold">불량명</span>
                        </div>
                        <select
                            bind:value={defectName}
                            class="select select-bordered w-full"
                        >
                            {#each defectTerms as term}
                                <option value={term}>{term}</option>
                            {/each}
                        </select>
                    </label>
                    <div class="flex items-end gap-2">
                        <button
                            class="btn btn-primary flex-1"
                            on:click={loadRankings}
                            disabled={loading}
                        >
                            {#if loading && !selectedEquipment}<span
                                    class="loading loading-spinner"
                                ></span>{/if}
                            조회
                        </button>
                        <button
                            class="btn btn-secondary flex-1"
                            on:click={handleIngest}
                            disabled={loading}
                        >
                            {#if loading && !selectedEquipment}<span
                                    class="loading loading-spinner"
                                ></span>{/if}
                            수집
                        </button>
                    </div>
                </div>

                <!-- Advanced Filters (Client Side) -->
                <div
                    class="collapse collapse-arrow bg-base-200 mt-4 rounded-box"
                >
                    <input type="checkbox" />
                    <div class="collapse-title text-md font-medium">
                        상세 필터
                    </div>
                    <div
                        class="collapse-content grid grid-cols-1 md:grid-cols-2 gap-4"
                    >
                        <label class="form-control">
                            <div class="label">
                                <span class="label-text"
                                    >공정 코드 (예: >1000, 100-200)</span
                                >
                            </div>
                            <input
                                type="text"
                                bind:value={processCodeFilter}
                                class="input input-bordered input-sm"
                                placeholder="Filter..."
                            />
                        </label>
                        <label class="form-control">
                            <div class="label">
                                <span class="label-text">설비 ID</span>
                            </div>
                            <input
                                type="text"
                                bind:value={equipmentFilter}
                                class="input input-bordered input-sm"
                                placeholder="Filter..."
                            />
                        </label>
                    </div>
                </div>
            </div>
        </div>

        {#if error}
            <div role="alert" class="alert alert-error mb-4">
                <svg
                    xmlns="http://www.w3.org/2000/svg"
                    class="stroke-current shrink-0 h-6 w-6"
                    fill="none"
                    viewBox="0 0 24 24"
                    ><path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="2"
                        d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z"
                    /></svg
                >
                <span>{error}</span>
            </div>
        {/if}

        <!-- Rankings Table -->
        <div class="card bg-base-100 shadow-xl mb-6 overflow-hidden">
            <div class="card-body p-0">
                <div class="overflow-x-auto max-h-96">
                    <table class="table table-zebra table-pin-rows">
                        <thead>
                            <tr>
                                <th>순위</th>
                                <th>설비</th>
                                <th>공정</th>
                                <th>제품 수</th>
                                <th>불량률</th>
                                <th>전체 평균</th>
                                <th>차이</th>
                            </tr>
                        </thead>
                        <tbody>
                            {#each paginatedRankings as rank, i}
                                <tr
                                    class:bg-base-200={selectedEquipment?.equipment_id ===
                                        rank.equipment_id}
                                >
                                    <th
                                        >{(currentPage - 1) * pageSize +
                                            i +
                                            1}</th
                                    >
                                    <td class="font-bold"
                                        >{rank.equipment_id}</td
                                    >
                                    <td>{rank.process_code}</td>
                                    <td
                                        >{rank.product_count.toLocaleString()}</td
                                    >
                                    <td>{rank.defect_rate.toFixed(3)}</td>
                                    <td
                                        >{rank.overall_defect_rate.toFixed(
                                            3,
                                        )}</td
                                    >
                                    <td
                                        class={rank.delta > 0
                                            ? "text-success font-bold"
                                            : "text-error font-bold"}
                                    >
                                        {rank.delta > 0
                                            ? "+"
                                            : ""}{rank.delta.toFixed(3)}
                                    </td>
                                </tr>
                            {:else}
                                <tr
                                    ><td
                                        colspan="7"
                                        class="text-center py-4 text-gray-500"
                                        >데이터가 없습니다</td
                                    ></tr
                                >
                            {/each}
                        </tbody>
                    </table>
                </div>

                <!-- Pagination & Download Controls -->
                <div
                    class="p-4 flex flex-col sm:flex-row justify-between items-center gap-4 bg-base-100 border-t"
                >
                    <!-- Download Button (Left) -->
                    <button
                        class="btn btn-sm btn-outline gap-2"
                        on:click={downloadExcel}
                    >
                        <svg
                            xmlns="http://www.w3.org/2000/svg"
                            class="h-4 w-4"
                            fill="none"
                            viewBox="0 0 24 24"
                            stroke="currentColor"
                        >
                            <path
                                stroke-linecap="round"
                                stroke-linejoin="round"
                                stroke-width="2"
                                d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
                            />
                        </svg>
                        엑셀 다운로드
                    </button>

                    <!-- Pagination (Right/Center) -->
                    <div class="join">
                        <button
                            class="join-item btn btn-sm"
                            disabled={currentPage === 1}
                            on:click={() => changePage(currentPage - 1)}
                            >«</button
                        >
                        <button class="join-item btn btn-sm"
                            >{currentPage} / {totalPages}</button
                        >
                        <button
                            class="join-item btn btn-sm"
                            disabled={currentPage === totalPages}
                            on:click={() => changePage(currentPage + 1)}
                            >»</button
                        >
                    </div>
                </div>
            </div>
        </div>

        <!-- Batch Analysis Cards -->
        <div class="divider">Detailed Analysis (Top 20)</div>

        {#if batchLoading}
            <center
                ><span class="loading loading-spinner loading-lg"
                ></span></center
            >
        {/if}

        {#if filteredRankings.length === 0}
            <div class="alert alert-warning">
                Debug: Filtered Rankings is 0. (Raw: {rankings.length})
            </div>
        {/if}

        <!-- Removed Keyed Each to prevent key errors -->
        {#each filteredRankings.slice(0, 20) as equipment, i}
            <div
                class="border-l-4 pl-2 mb-4 {batchResults &&
                batchResults[equipment.equipment_id]
                    ? 'border-success'
                    : 'border-warning'}"
            >
                <div class="text-xs text-gray-400 mb-1 flex gap-2">
                    <span>ID: {equipment.equipment_id}</span>
                    <span
                        >Has Res: {batchResults &&
                        batchResults[equipment.equipment_id]
                            ? "YES"
                            : "NO"}</span
                    >
                </div>
                <AnalysisCard
                    {equipment}
                    results={batchResults
                        ? batchResults[equipment.equipment_id]
                        : null}
                />
            </div>
        {/each}
    </div>

    <!-- Toast Notification -->
    {#if toast}
        <div class="toast toast-bottom toast-end z-50">
            <div class="alert alert-{toast.type}">
                <span>{toast.message}</span>
            </div>
        </div>
    {/if}
</div>
