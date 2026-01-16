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
        getHeatmapConfig,
        updateHeatmapConfig,
        getExportUrl,
    } from "./api.js";
    import AnalysisCard from "./AnalysisCard.svelte";

    import Plotly from "plotly.js-dist-min";

    export let config;

    // Facility State
    let facilities = [];
    let selectedFacility = "";

    // Initialization
    $: if (config?.Settings?.Facilities) {
        facilities = config.Settings.Facilities;
        // Set default if not set
        if (!selectedFacility && facilities.length > 0) {
            selectedFacility = facilities[0];
            // Maybe trigger a reload or toast? "Facility set to A1T"
        } else if (!selectedFacility) {
            selectedFacility = "default";
        }
    } else if (!selectedFacility) {
        // Fallback checks
        selectedFacility = "default";
    }

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

    // Model Filter & Grid Config
    let availableModels = [];
    let selectedModels = []; // Array of strings
    let showGridModal = false;
    let gridConfigData = {}; // Map<Model, {x_list, y_list}>
    let activeGridModel = "";
    let gridXInput = "";
    let gridYInput = "";
    let modelSearchQuery = "";
    let newModelName = "";

    $: filteredGridModels = availableModels.filter((m) =>
        m.toLowerCase().includes(modelSearchQuery.toLowerCase()),
    );

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
            "모델",
            "불량률",
            "차이",
            "총 불량 수",
            "제품 수", // Renamed from Glass Count
        ];

        // Map Data
        const rows = filteredRankings.map((r, index) => [
            index + 1,
            r.equipment_id,
            r.process_code,
            r.model_code,
            (r.defect_rate * 100).toFixed(4) + "%",
            (r.delta * 100).toFixed(4) + "%",
            r.total_defects,
            r.product_count,
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

    $: if (config && config.MockData?.Products) {
        availableModels = config.MockData.Products;
    }

    // Client-side filtering (Pure Reactive)
    $: filteredRankings = getFilteredRankings(
        rankings,
        processCodeFilter,
        equipmentFilter,
    );

    onMount(async () => {
        await loadRankings(); // Auto-load enabled
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
            model_codes: selectedModels,
            facility_code: selectedFacility, // Added
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
                model_codes: selectedModels, // ADDED
                facility_code: selectedFacility, // ADDED
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

        // 2. Daily Trend
        if (dailyChartDiv && results.daily_results) {
            const dailyData = results.daily_results || [];
            dailyData.sort(
                (a, b) => new Date(a.work_date) - new Date(b.work_date),
            );

            const traceDaily = {
                x: dailyData.map((d) => d.work_date),
                y: dailyData.map((d) => d.total_defects),
                type: "scatter",
                mode: "lines+markers",
                name: "Defects",
                line: { color: "#3498db" },
            };

            const layoutDaily = {
                title: "Daily Defect Trend",
                xaxis: {
                    tickformat: "%Y-%m-%d",
                    tickangle: -45,
                },
                yaxis: { title: "Total Defects" },
                margin: { t: 30, l: 50, r: 20, b: 80 },
                height: 400,
            };

            Plotly.newPlot(dailyChartDiv, [traceDaily], layoutDaily);
        }

        // 3. Heatmap
        // Only render if container exists (might be hidden for multiple models)
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
        // Allow lookup without specific models
        // if (selectedModels.length === 0) {
        //     showToast("모델을 선택해주세요.", "error");
        //     return;
        // }

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
                facility: selectedFacility, // Added
            });
            rankings = data.rankings || [];

            if (rankings.length === 0) {
                showToast("검색 결과가 없습니다.", "info");
            } else {
                showToast(
                    `조회 완료: ${rankings.length}건 (` +
                        selectedFacility +
                        ")",
                    "success",
                );
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

    async function openGridSettings() {
        showGridModal = true;
        try {
            const data = await getHeatmapConfig();
            // gridConfigData is map[model] -> {x_list, y_list}
            // Ensure object
            gridConfigData = data?.configs || data || {};
            // Select first model if available
            if (!activeGridModel && availableModels.length > 0) {
                activeGridModel = availableModels[0];
                loadGridInputs(activeGridModel);
            }
        } catch (e) {
            console.error(e);
            showToast("설정 로드 실패", "error");
        }
    }

    function loadGridInputs(model) {
        if (!model) return;
        const cfg = gridConfigData[model] || { x_list: [], y_list: [] };
        gridXInput = (cfg.x_list || []).join(",");
        gridYInput = (cfg.y_list || []).join(",");
    }

    function changeActiveGridModel(model) {
        activeGridModel = model;
        loadGridInputs(model);
    }

    async function saveGridSettings() {
        // Update current active model first
        if (activeGridModel) {
            gridConfigData[activeGridModel] = {
                x_list: gridXInput
                    .split(",")
                    .map((s) => s.trim())
                    .filter((s) => s),
                y_list: gridYInput
                    .split(",")
                    .map((s) => s.trim())
                    .filter((s) => s),
            };
        }

        try {
            await updateHeatmapConfig(gridConfigData);
            showToast("Grid 설정이 저장되었습니다.", "success");
            showGridModal = false;
        } catch (e) {
            console.error(e);
            showToast("저장 실패", "error");
        }
    }

    function addNewModel() {
        if (!newModelName) return;
        if (availableModels.includes(newModelName)) {
            showToast("이미 존재하는 모델입니다.", "warning");
            return;
        }
        availableModels = [...availableModels, newModelName];
        gridConfigData[newModelName] = { x_list: [], y_list: [] };
        activeGridModel = newModelName;
        loadGridInputs(newModelName);
        newModelName = "";
        modelSearchQuery = ""; // Clear search to show new model
    }

    // Toggle Model Selection
    function toggleModel(model) {
        if (selectedModels.includes(model)) {
            selectedModels = selectedModels.filter((m) => m !== model);
        } else {
            selectedModels = [...selectedModels, model];
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
    <!-- Header with Theme Toggle -->
    <div class="navbar bg-base-100 mb-6 rounded-box shadow-md">
        <div class="flex-1">
            <h1 class="text-2xl font-bold px-4">
                Display Manufacturing Analysis
            </h1>
        </div>
        <div class="flex-none">
            <!-- Theme Toggle: Unchecked=Black(Dark), Checked=Corporate(Light) -->
            <label class="swap swap-rotate btn btn-ghost btn-circle">
                <input
                    type="checkbox"
                    class="theme-controller"
                    value="corporate"
                    checked
                />

                <!-- Sun Icon (Visible when Light/Checked) -->
                <svg
                    class="swap-on fill-current w-6 h-6"
                    xmlns="http://www.w3.org/2000/svg"
                    viewBox="0 0 24 24"
                    ><path
                        d="M5.64,17l-.71.71a1,1,0,0,0,0,1.41,1,1,0,0,0,1.41,0l.71-.71A1,1,0,0,0,5.64,17ZM5,12a1,1,0,0,0-1-1H3a1,1,0,0,0,0,2H4A1,1,0,0,0,5,12Zm7-7a1,1,0,0,0,1-1V3a1,1,0,0,0-2,0V4A1,1,0,0,0,12,5ZM5.64,7.05a1,1,0,0,0,.7.29,1,1,0,0,0,.71-.29,1,1,0,0,0,0-1.41l-.71-.71A1,1,0,0,0,5.64,7.05Zm12,1.41a1,1,0,0,0,.7.29,1,1,0,0,0,.71-.29l.71-.71a1,1,0,0,0,0-1.41l-.71-.71A1,1,0,0,0,17.64,7.05Zm1.06,10.9a1,1,0,0,0,0,1.41,1,1,0,0,0,1.41,0l.71-.71a1,1,0,0,0,0-1.41Zm-9.19,2.44a1,1,0,0,0,1.41,0,1,1,0,0,0,0-1.41l-.71-.71a1,1,0,0,0-1.41,0,1,1,0,0,0,0,1.41ZM12,22a1,1,0,0,0,1-1V19a1,1,0,0,0-2,0v2A1,1,0,0,0,12,22Zm8-9a1,1,0,0,0,1,1h1a1,1,0,0,0,0-2H21A1,1,0,0,0,20,13Zm-9.5,6.69A8.14,8.14,0,0,1,7.08,5.22v.27A10.15,10.15,0,0,0,17.22,15.63a9.79,9.79,0,0,0,2.1-.22A8.11,8.11,0,0,1,10.5,19.69Z"
                    /></svg
                >

                <!-- Moon Icon (Visible when Dark/Unchecked) -->
                <svg
                    class="swap-off fill-current w-6 h-6"
                    xmlns="http://www.w3.org/2000/svg"
                    viewBox="0 0 24 24"
                    ><path
                        d="M21.64,13a1,1,0,0,0-1.05-.14,8.05,8.05,0,0,1-3.37.73A8.15,8.15,0,0,1,9.08,5.49a8.59,8.59,0,0,1,.25-2A1,1,0,0,0,8,2.36,10.14,10.14,0,1,0,22,14.05,1,1,0,0,0,21.64,13Zm-9.5,6.69A8.14,8.14,0,0,1,7.08,5.22v.27A10.15,10.15,0,0,0,17.22,15.63a9.79,9.79,0,0,0,2.1-.22A8.11,8.11,0,0,1,10.5,19.69Z"
                    /></svg
                >
            </label>
        </div>
    </div>

    <!-- DASHBOARD CONTENT -->
    <div class="">
        <!-- Controls -->
        <div class="card bg-base-100 shadow-xl mb-6 rounded-2xl">
            <div class="card-body">
                <div
                    class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4"
                >
                    {#if facilities.length > 0}
                        <label class="form-control w-full">
                            <div class="label">
                                <span class="label-text font-bold">공장</span>
                            </div>
                            <select
                                bind:value={selectedFacility}
                                class="select select-bordered w-full rounded-xl"
                            >
                                {#each facilities as fac}
                                    <option value={fac}>{fac}</option>
                                {/each}
                            </select>
                        </label>
                    {/if}
                    <label class="form-control w-full">
                        <div class="label">
                            <span class="label-text font-bold">시작일</span>
                        </div>
                        <input
                            type="date"
                            bind:value={startDate}
                            class="input input-bordered w-full rounded-xl"
                        />
                    </label>
                    <label class="form-control w-full">
                        <div class="label">
                            <span class="label-text font-bold">종료일</span>
                        </div>
                        <input
                            type="date"
                            bind:value={endDate}
                            class="input input-bordered w-full rounded-xl"
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

                    <!-- New Model Filter -->
                    <div class="form-control w-full relative">
                        <div class="label flex justify-between">
                            <span class="label-text font-bold">모델</span>
                            <button
                                class="btn btn-xs btn-ghost text-gray-500"
                                on:click|stopPropagation={openGridSettings}
                                title="Heatmap Grid Settings">⚙️</button
                            >
                        </div>
                        <div class="dropdown dropdown-bottom w-full">
                            <div
                                tabindex="0"
                                role="button"
                                class="btn btn-outline btn-sm w-full justify-between font-normal"
                            >
                                <span class="truncate"
                                    >{selectedModels.length > 0
                                        ? selectedModels.join(", ")
                                        : "전체 모델"}</span
                                >
                                <span class="text-xs">▼</span>
                            </div>
                            <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
                            <ul
                                tabindex="0"
                                class="dropdown-content z-[999] menu p-2 shadow bg-base-100 rounded-box w-full max-h-60 overflow-y-auto block border border-gray-200"
                            >
                                {#each availableModels as model}
                                    <li>
                                        <label
                                            class="label cursor-pointer justify-start gap-2 hover:bg-base-200"
                                        >
                                            <input
                                                type="checkbox"
                                                class="checkbox checkbox-xs"
                                                checked={selectedModels.includes(
                                                    model,
                                                )}
                                                on:change={() =>
                                                    toggleModel(model)}
                                            />
                                            <span class="label-text"
                                                >{model}</span
                                            >
                                        </label>
                                    </li>
                                {/each}
                            </ul>
                        </div>
                    </div>

                    <div class="flex items-end gap-2">
                        <button
                            class="btn btn-primary flex-1 rounded-xl"
                            on:click={loadRankings}
                            disabled={loading}
                        >
                            {#if loading && !selectedEquipment}<span
                                    class="loading loading-spinner"
                                ></span>{/if}
                            분석
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
                                <span class="label-text">공정 코드</span>
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
                                <th>모델</th>
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
                                    <td>{rank.model_code}</td>
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
                                        colspan="8"
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

        <!-- Single Analysis Results (Charts) -->
        {#if analysisResults}
            <div class="divider">
                Analysis Results ({analysisResults.job_id})
            </div>

            <div class="flex justify-end gap-2 px-4 mb-2">
                {#if analysisResults.job_id}
                    <a
                        href={getExportUrl(analysisResults.job_id)}
                        target="_blank"
                        class="btn btn-sm btn-success text-white"
                    >
                        <svg
                            xmlns="http://www.w3.org/2000/svg"
                            class="h-4 w-4 mr-1"
                            fill="none"
                            viewBox="0 0 24 24"
                            stroke="currentColor"
                        >
                            <path
                                stroke-linecap="round"
                                stroke-linejoin="round"
                                stroke-width="2"
                                d="M12 10v6m0 0l-3-3m3 3l3-3m2 8H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                            />
                        </svg>
                        Export CSV
                    </a>
                {/if}
            </div>

            <div class="grid grid-cols-1 lg:grid-cols-2 gap-4 p-4">
                <div class="card bg-base-100 shadow-xl">
                    <div class="card-body p-2">
                        <h3 class="card-title text-sm">
                            Glass Defects (Scatter)
                        </h3>
                        <div bind:this={glassChartDiv}></div>
                    </div>
                </div>
                <div class="card bg-base-100 shadow-xl">
                    <div class="card-body p-2">
                        <h3 class="card-title text-sm">Daily Trend</h3>
                        <div bind:this={dailyChartDiv}></div>
                    </div>
                </div>
                <div class="card bg-base-100 shadow-xl lg:col-span-2">
                    <div class="card-body p-2">
                        <h3 class="card-title text-sm">Heatmap</h3>
                        {#if selectedModels.length > 1}
                            <div class="alert alert-info text-xs">
                                Multiple models selected. Heatmap disabled.
                            </div>
                        {:else}
                            <div bind:this={heatmapChartDiv}></div>
                        {/if}
                    </div>
                </div>
            </div>
        {/if}

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
                    <span class="font-bold text-primary">Rank {i + 1}</span>
                    <span>ID: {equipment.equipment_id}</span>
                    <span class="badge badge-sm badge-outline"
                        >{equipment.model_code}</span
                    >
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

    <!-- Grid Settings Modal -->
    <dialog class="modal" class:modal-open={showGridModal}>
        <div class="modal-box w-11/12 max-w-3xl">
            <h3 class="font-bold text-lg">패널주소 설정</h3>
            <p class="py-4 text-sm text-gray-500">
                모델별 히트맵 Grid (X/Y축 순서)를 설정합니다. 설정된 순서대로
                히트맵이 고정되어 출력됩니다.
            </p>

            <!-- Search and Add Model -->
            <div class="flex gap-2 mb-4">
                <input
                    type="text"
                    bind:value={modelSearchQuery}
                    class="input input-sm input-bordered flex-1"
                    placeholder="모델 검색..."
                />
                <div class="flex gap-2">
                    <input
                        type="text"
                        bind:value={newModelName}
                        class="input input-sm input-bordered"
                        placeholder="새 모델명"
                    />
                    <button
                        class="btn btn-sm btn-secondary"
                        on:click={addNewModel}>추가</button
                    >
                </div>
            </div>

            <div class="flex gap-2 mb-4 overflow-x-auto pb-2">
                {#each filteredGridModels as model}
                    <button
                        class="btn btn-sm {activeGridModel === model
                            ? 'btn-primary'
                            : 'btn-outline text-gray-400'}"
                        on:click={() => changeActiveGridModel(model)}
                        >{model}</button
                    >
                {/each}
                {#if filteredGridModels.length === 0}
                    <span class="text-sm text-gray-400 py-1"
                        >검색 결과가 없습니다.</span
                    >
                {/if}
            </div>

            {#if activeGridModel}
                <div class="card bg-base-200 p-4">
                    <span class="font-bold mb-2 block text-primary"
                        >{activeGridModel} 설정</span
                    >
                    <div class="form-control">
                        <div class="label">
                            <span class="label-text"
                                >X Axis (Comma separated, e.g. A,B,C)</span
                            >
                        </div>
                        <input
                            type="text"
                            bind:value={gridXInput}
                            class="input input-bordered"
                            placeholder="A,B,C..."
                        />
                    </div>
                    <div class="form-control mt-4">
                        <div class="label">
                            <span class="label-text"
                                >Y Axis (Comma separated, e.g. 1,2,3)</span
                            >
                        </div>
                        <input
                            type="text"
                            bind:value={gridYInput}
                            class="input input-bordered"
                            placeholder="1,2,3..."
                        />
                    </div>
                </div>
            {:else}
                <div class="alert alert-info">모델을 선택해주세요.</div>
            {/if}

            <div class="modal-action">
                <button class="btn btn-primary" on:click={saveGridSettings}
                    >저장</button
                >
                <button class="btn" on:click={() => (showGridModal = false)}
                    >닫기</button
                >
            </div>
        </div>
        <form method="dialog" class="modal-backdrop">
            <button on:click={() => (showGridModal = false)}>close</button>
        </form>
    </dialog>
</div>
