<script>
    import { onMount } from "svelte";
    import {
        getConfig,
        getHeatmapConfig,
        updateHeatmapConfig,
        getSchedulerConfig,
        updateSchedulerConfig,
        triggerIngest,
        refreshMart,
        analyzeHierarchy,
        getHierarchyExportUrl,
    } from "./api.js";
    import HierarchyResultCard from "./HierarchyResultCard.svelte";
    import { theme, chartMode, routeAnalysisProducts } from "./store.js";

    export let config;

    let facilities = [];
    let selectedFacility = "";

    $: if (config?.Settings?.Facilities) {
        facilities = config.Settings.Facilities;
        if (!selectedFacility && facilities.length > 0) {
            selectedFacility = facilities[0];
        } else if (!selectedFacility) {
            selectedFacility = "default";
        }
    } else if (!selectedFacility) {
        selectedFacility = "default";
    }

    let loading = false;
    let error = null;

    let hierarchyResults = [];
    let hierarchySessionId = null;

    let availableModels = [];
    let selectedModel = "";
    let showGridModal = false;
    let gridConfigData = {};
    let activeGridModel = "";
    let gridXInput = "";
    let gridYInput = "";
    let modelSearchQuery = "";
    let newModelName = "";

    let showIngestModal = false;
    let schedulerConfig = { enabled: true, interval_minutes: 60 };
    let manualIngestMode = "incremental";
    let manualStart = "";
    let manualEnd = "";

    $: filteredGridModels = availableModels.filter((m) =>
        m.toLowerCase().includes(modelSearchQuery.toLowerCase()),
    );

    let currentPage = 1;
    let pageSize = 20;

    $: paginatedResults = hierarchyResults.slice(
        (currentPage - 1) * pageSize,
        currentPage * pageSize,
    );
    $: totalPages = Math.max(1, Math.ceil(hierarchyResults.length / pageSize));

    let previousResultsLength = 0;
    $: if (hierarchyResults.length !== previousResultsLength) {
        currentPage = 1;
        previousResultsLength = hierarchyResults.length;
    }

    function changePage(newPage) {
        if (newPage >= 1 && newPage <= totalPages) {
            currentPage = newPage;
        }
    }

    let today = new Date();
    let twoWeeksAgo = new Date(today.getTime() - 14 * 24 * 60 * 60 * 1000);
    let startDate = twoWeeksAgo.toISOString().split("T")[0];
    let endDate = today.toISOString().split("T")[0];

    let defectTerms = config?.Settings?.DefectTerms || [];
    let defectName = defectTerms[0] || "";
    let analysisLevel = "path";

    let toast = null;

    $: if (config && config.Settings?.DefectTerms) {
        defectTerms = config.Settings.DefectTerms;
        if (!defectName && defectTerms.length > 0) defectName = defectTerms[0];
    }

    $: if (config && config.MockData?.Products) {
        availableModels = config.MockData.Products;
        if (!selectedModel && availableModels.length > 0) {
            selectedModel = availableModels[0];
        }
    }

    function showToast(message, type = "info") {
        toast = { message, type };
        setTimeout(() => {
            toast = null;
        }, 3000);
    }

    $: if ($routeAnalysisProducts && $routeAnalysisProducts.length > 0) {
        const pIds = [...$routeAnalysisProducts];
        routeAnalysisProducts.set([]); // Clear so it only runs once per click
        runHierarchyAnalysis(pIds);
    }

    async function runHierarchyAnalysis(customProductIds = null) {
        if (
            !selectedModel &&
            !(customProductIds && customProductIds.length > 0)
        ) {
            showToast("모델을 선택해주세요.", "error");
            return;
        }

        loading = true;
        error = null;
        hierarchyResults = [];
        hierarchySessionId = null;

        try {
            const params = {
                facility: selectedFacility,
                start: startDate, // Date is still passed but backend gives priority to product_ids
                end: endDate,
                model_code: selectedModel,
                defect_name: defectName,
                analysis_level: analysisLevel,
            };

            if (customProductIds && customProductIds.length > 0) {
                params.product_ids = customProductIds;
                showToast(
                    `🔗선택된 패턴 영역의 ${customProductIds.length}개 품목 기준 Route 분석을 시작합니다.`,
                    "info",
                );
            }

            const resp = await analyzeHierarchy(params);

            hierarchyResults = resp.data || [];
            hierarchySessionId = resp.session_id || null;

            if (hierarchyResults.length === 0) {
                showToast("검색 결과가 없습니다.", "info");
            } else {
                showToast(`분석 완료: ${hierarchyResults.length}건`, "success");
            }
        } catch (e) {
            console.error("Hierarchy Analysis Error:", e);
            error = e.message;
            showToast("분석 실패: " + e.message, "error");
        } finally {
            loading = false;
        }
    }

    function downloadAllCharts() {
        if (!hierarchySessionId) {
            showToast("분석을 먼저 실행하세요.", "error");
            return;
        }
        const url = getHierarchyExportUrl(hierarchySessionId);
        window.open(url, "_blank");
    }

    function downloadCSV() {
        if (!hierarchyResults || hierarchyResults.length === 0) return;

        const headers = [
            "Process",
            "Line",
            "Machine",
            "Path",
            "Products",
            "Defects",
            "DPU",
        ];

        const rows = hierarchyResults.map((r) => [
            r.process_code,
            r.equipment_line_id || "",
            r.equipment_machine_id || "",
            r.equipment_path_id || "",
            r.total_products,
            r.total_defects,
            r.dpu?.toFixed(6),
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
            `hierarchy_${selectedModel}_${startDate}_${endDate}.csv`,
        );
        link.style.visibility = "hidden";
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    }

    async function openGridSettings() {
        showGridModal = true;
        try {
            const data = await getHeatmapConfig();
            gridConfigData = data?.configs || data || {};
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
        modelSearchQuery = "";
    }

    async function openIngestModal() {
        showIngestModal = true;
        try {
            const cfg = await getSchedulerConfig();
            schedulerConfig = cfg;
        } catch (e) {
            console.error(e);
            showToast("스케줄러 설정 로드 실패", "error");
        }
    }

    async function saveSchedulerSettings() {
        try {
            schedulerConfig.interval_minutes = parseInt(
                schedulerConfig.interval_minutes,
            );
            await updateSchedulerConfig(schedulerConfig);
            showToast("스케줄러 설정 저장됨", "success");
        } catch (e) {
            showToast("설정 저장 실패: " + e.message, "error");
        }
    }

    async function runManualIngest() {
        loading = true;
        showIngestModal = false;
        try {
            let start = "";
            let end = "";

            if (manualIngestMode === "custom") {
                if (!manualStart || !manualEnd) {
                    throw new Error("시작일과 종료일을 입력해주세요.");
                }
                start = manualStart;
                end = manualEnd;
                showToast(`수동 수집 시작 (${start} ~ ${end})`, "info");
            } else {
                showToast("증분 수집 시작 (최신 데이터 이후)", "info");
            }

            const counts = await triggerIngest(start, end);

            let msg = "수집 완료: ";
            for (const [k, v] of Object.entries(counts)) {
                msg += `${k}=${v} `;
            }
            showToast(msg, "success");

            await refreshMart();
            showToast("데이터 마트 갱신 완료", "success");
        } catch (e) {
            console.error(e);
            showToast("수집 실패: " + e.message, "error");
        } finally {
            loading = false;
        }
    }
</script>

<div class="p-6">
    <div class="navbar bg-base-100 mb-6 rounded-box shadow-md">
        <div class="flex-1">
            <h1 class="text-2xl font-bold px-4">뚜냔 AI 프로젝트</h1>
        </div>
        <div class="flex-none gap-2">
            <label class="swap swap-rotate text-primary">
                <input
                    type="checkbox"
                    class="theme-controller"
                    value="lgd-dark"
                    checked={$theme === "lgd-dark"}
                    on:change={(e) =>
                        theme.set(e.target.checked ? "lgd-dark" : "corporate")}
                />
                <svg
                    class="swap-on fill-current w-6 h-6"
                    xmlns="http://www.w3.org/2000/svg"
                    viewBox="0 0 24 24"
                    ><path
                        d="M5.64,17l-.71.71a1,1,0,0,0,0,1.41,1,1,0,0,0,1.41,0l.71-.71A1,1,0,0,0,5.64,17ZM5,12a1,1,0,0,0-1-1H3a1,1,0,0,0,0,2H4A1,1,0,0,0,5,12Zm7-7a1,1,0,0,0,1-1V3a1,1,0,0,0-2,0V4A1,1,0,0,0,12,5ZM5.64,7.05a1,1,0,0,0,.7.29,1,1,0,0,0,.71-.29,1,1,0,0,0,0-1.41l-.71-.71A1,1,0,0,0,5.64,7.05Zm12,1.41a1,1,0,0,0,.7.29,1,1,0,0,0,.71-.29l.71-.71a1,1,0,0,0,0-1.41l-.71-.71A1,1,0,0,0,17.64,7.05Zm1.06,10.9a1,1,0,0,0,0,1.41,1,1,0,0,0,1.41,0l.71-.71a1,1,0,0,0,0-1.41Zm-9.19,2.44a1,1,0,0,0,1.41,0,1,1,0,0,0,0-1.41l-.71-.71a1,1,0,0,0-1.41,0,1,1,0,0,0,0,1.41ZM12,22a1,1,0,0,0,1-1V19a1,1,0,0,0-2,0v2A1,1,0,0,0,12,22Zm8-9a1,1,0,0,0,1,1h1a1,1,0,0,0,0-2H21A1,1,0,0,0,20,13Zm-9.5,6.69A8.14,8.14,0,0,1,7.08,5.22v.27A10.15,10.15,0,0,0,17.22,15.63a9.79,9.79,0,0,0,2.1-.22A8.11,8.11,0,0,1,10.5,19.69Z"
                    /></svg
                >
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

    <div class="">
        <div class="card bg-base-100 shadow-xl mb-6 rounded-2xl">
            <div class="card-body">
                <div
                    class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-7 gap-4"
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

                    <div class="form-control w-full">
                        <div class="label flex justify-between">
                            <span class="label-text font-bold">모델</span>
                            <button
                                class="btn btn-xs btn-ghost text-gray-500"
                                on:click|stopPropagation={openGridSettings}
                                title="Heatmap Grid Settings">⚙️</button
                            >
                        </div>
                        <select
                            bind:value={selectedModel}
                            class="select select-bordered w-full rounded-xl"
                        >
                            <option value="">모델 선택 (필수)</option>
                            {#each availableModels as model}
                                <option value={model}>{model}</option>
                            {/each}
                        </select>
                    </div>

                    <label class="form-control w-full">
                        <div class="label">
                            <span class="label-text font-bold">분석 레벨</span>
                        </div>
                        <select
                            bind:value={analysisLevel}
                            class="select select-bordered w-full rounded-xl"
                        >
                            <option value="process">공정 (Process)</option>
                            <option value="line">라인 (Line)</option>
                            <option value="machine">설비 (Machine)</option>
                            <option value="path">경로 (Path)</option>
                        </select>
                    </label>

                    <div class="flex items-end gap-2">
                        <button
                            class="btn btn-primary flex-1 rounded-xl"
                            on:click={() => runHierarchyAnalysis(null)}
                            disabled={loading}
                        >
                            {#if loading}<span class="loading loading-spinner"
                                ></span>{/if}
                            분석
                        </button>
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

        {#if hierarchyResults.length > 0}
            <div class="flex justify-between items-center mb-4">
                <div class="text-sm text-base-content/60">
                    {hierarchyResults.length}건 결과
                </div>
                <div class="flex gap-2 items-center">
                    <div class="join">
                        <button
                            class="join-item btn btn-sm {$chartMode === 'image'
                                ? 'btn-active btn-primary'
                                : ''}"
                            on:click={() => chartMode.set("image")}
                        >
                            Image
                        </button>
                        <button
                            class="join-item btn btn-sm {$chartMode ===
                            'interactive'
                                ? 'btn-active btn-primary'
                                : ''}"
                            on:click={() => chartMode.set("interactive")}
                        >
                            Interactive
                        </button>
                    </div>
                    <button
                        class="btn btn-sm btn-outline gap-1"
                        on:click={downloadCSV}
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
                        CSV
                    </button>
                    {#if hierarchySessionId}
                        <button
                            class="btn btn-sm btn-outline btn-success gap-1"
                            on:click={downloadAllCharts}
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
                            Charts ZIP
                        </button>
                    {/if}
                </div>
            </div>

            <div class="card bg-base-100 shadow-xl mb-6 overflow-hidden">
                <div class="card-body p-0">
                    <div class="overflow-x-auto max-h-72">
                        <table
                            class="table table-zebra table-pin-rows table-sm"
                        >
                            <thead>
                                <tr>
                                    <th>#</th>
                                    <th>공정</th>
                                    <th>라인</th>
                                    <th>설비</th>
                                    <th>경로</th>
                                    <th>제품 수</th>
                                    <th>불량 수</th>
                                    <th>DPU</th>
                                </tr>
                            </thead>
                            <tbody>
                                {#each paginatedResults as r, i}
                                    <tr>
                                        <th
                                            >{(currentPage - 1) * pageSize +
                                                i +
                                                1}</th
                                        >
                                        <td>{r.process_code}</td>
                                        <td>{r.equipment_line_id || "-"}</td>
                                        <td>{r.equipment_machine_id || "-"}</td>
                                        <td>{r.equipment_path_id || "-"}</td>
                                        <td
                                            >{r.total_products?.toLocaleString()}</td
                                        >
                                        <td
                                            >{r.total_defects?.toLocaleString()}</td
                                        >
                                        <td class="font-mono"
                                            >{r.dpu?.toFixed(4)}</td
                                        >
                                    </tr>
                                {:else}
                                    <tr>
                                        <td
                                            colspan="8"
                                            class="text-center py-4 text-gray-500"
                                            >데이터가 없습니다</td
                                        >
                                    </tr>
                                {/each}
                            </tbody>
                        </table>
                    </div>

                    <div
                        class="p-4 flex justify-end items-center gap-4 bg-base-100 border-t"
                    >
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

            <div class="divider">DPU Trend Charts</div>

            {#each hierarchyResults as result, i}
                <HierarchyResultCard {result} index={i} />
            {/each}
        {:else if !loading}
            <div
                class="flex flex-col items-center justify-center h-48 text-base-content/40"
            >
                <p>분석 조건을 설정하고 "분석" 버튼을 클릭하세요.</p>
            </div>
        {/if}

        {#if loading}
            <div class="flex flex-col items-center justify-center py-12">
                <span class="loading loading-spinner loading-lg"></span>
                <p class="mt-4 text-base-content/60">분석 중...</p>
            </div>
        {/if}
    </div>

    {#if toast}
        <div class="toast toast-bottom toast-end z-50">
            <div class="alert alert-{toast.type}">
                <span>{toast.message}</span>
            </div>
        </div>
    {/if}

    <dialog class="modal" class:modal-open={showGridModal}>
        <div class="modal-box w-11/12 max-w-3xl">
            <h3 class="font-bold text-lg">Heatmap 라벨 설정</h3>
            <p class="py-4 text-sm text-gray-500">
                모델별 히트맵 Grid (X/Y축 순서)를 설정합니다. 설정된 순서대로
                히트맵이 고정되어 출력됩니다.
            </p>

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
                        >{activeGridModel} Heatmap 라벨 설정</span
                    >
                    <div class="form-control">
                        <div class="label">
                            <span class="label-text"
                                >X축 라벨 (쉼표로 구분, 예: A,B,C)</span
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
                                >Y축 라벨 (쉼표로 구분, 예: 1,2,3)</span
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

<style>
    :global(html) {
        transition:
            background-color 0.3s ease,
            color 0.3s ease;
    }
    :global(.card) {
        border: 1px solid transparent;
        transition: all 0.3s ease;
    }
    :global([data-theme="lgd-dark"] .card) {
        border-color: #334155;
        box-shadow:
            0 4px 6px -1px rgba(0, 0, 0, 0.5),
            0 2px 4px -1px rgba(0, 0, 0, 0.3);
    }
</style>
