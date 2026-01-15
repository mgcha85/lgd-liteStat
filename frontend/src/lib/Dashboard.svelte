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

    // Filters
    // Default to 2025-06-01 to match current mock data range (June 2025 - Jan 2026)
    let startDate = "2025-06-01";
    let endDate = new Date().toISOString().split("T")[0];

    // Defect Name (Dropdown)
    let defectTerms = config?.Settings?.DefectTerms || [];
    let defectName = defectTerms[0] || "";

    // Advanced Filters
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
        await loadRankings();
    });

    // Reactive Trigger for Batch Analysis
    $: if (
        filteredRankings &&
        filteredRankings.length > 0 &&
        !batchAttempted &&
        !batchLoading &&
        !loading
    ) {
        console.log(
            "Reactive Trigger: Starting Sequential Analysis... Len:",
            filteredRankings.length,
        );
        runSequentialAnalysis(filteredRankings.slice(0, 20));
    }

    function getFilteredRankings(data, pFilter, eFilter) {
        if (!data) return [];
        console.log("getFilteredRankings DEBUG. In:", data.length);
        console.log(
            "Filters Raw:",
            JSON.stringify(pFilter),
            JSON.stringify(eFilter),
        );

        // TEMPORARY: Bypass filters to force rendering
        return data;

        /*
        let res = data;

        // Equipment Filter (Simple substring)
        if (eFilter && eFilter.trim()) {
            const lower = eFilter.trim().toLowerCase();
            res = res.filter((r) =>
                r.equipment_id.toLowerCase().includes(lower),
            );
        }

        // Process Code Range Filter
        if (pFilter && pFilter.trim()) {
            const filter = pFilter.trim();
            res = res.filter((r) => checkProcessFilter(r.process_code, filter));
        }

        console.log("Filtered Result:", res.length);
        return res;
        */
    }

    function checkProcessFilter(code, filter) {
        // Try to parse code as number
        const val = parseInt(code.replace(/[^0-9]/g, "")); // Strip P if P100 -> 100
        if (isNaN(val)) return code.includes(filter); // Text fallback

        if (filter.startsWith(">")) {
            const limit = parseInt(filter.substring(1));
            return !isNaN(limit) ? val > limit : true;
        } else if (filter.startsWith("<")) {
            const limit = parseInt(filter.substring(1));
            return !isNaN(limit) ? val < limit : true;
        } else if (filter.includes("-")) {
            const parts = filter.split("-");
            const min = parseInt(parts[0]);
            const max = parseInt(parts[1]);
            return !isNaN(min) && !isNaN(max) ? val >= min && val <= max : true;
        }
        return code.includes(filter);
    }

    let batchResults = {}; // Map<EquipmentID, Results>
    let batchLoading = false;
    let batchError = null;
    let batchAttempted = false;

    async function loadRankings() {
        loading = true;
        error = null;
        batchResults = {}; // Clear previous results
        try {
            const config = await getConfig();
            const topN = config.analysis?.top_n_limit || 20;

            // Fetch more than limit to allow client filtering
            const data = await getEquipmentRankings({
                start_date: startDate,
                end_date: endDate,
                defect_name: defectName,
                limit: topN, // Fetch more for client filtering
            });
            rankings = data.rankings || [];
            // filterRankings call removed (Handled by reactive statement)

            console.log("Rankings Loaded:", rankings.length);
            console.log("Filtered Rankings:", filteredRankings.length);

            batchAttempted = false; // Reset attempt flag to allow reactive trigger
        } catch (e) {
            console.error("Load Rankings Error:", e);
            error = e.message;
        } finally {
            loading = false;
        }
    }

    const BATCH_CHUNK_SIZE = 5; // Process 5 equipments at a time
    let processedCount = 0;
    let totalTargets = 0;

    async function runSequentialAnalysis(targets) {
        if (targets.length === 0) return;

        batchLoading = true;
        batchError = null;
        processedCount = 0;
        totalTargets = targets.length;

        // Clear previous results ONLY if fresh start
        if (!batchAttempted) {
            batchResults = {};
        }

        console.log(
            `Starting SEQUENTIAL analysis for ${targets.length} targets`,
        );

        try {
            for (const target of targets) {
                // If we already have a result, skip (or overwrite if force reload?)
                // For now, simple skip to avoid re-fetching on reactivity triggers
                if (batchResults[target.equipment_id]) {
                    processedCount++;
                    continue;
                }

                const req = {
                    defect_name: defectName,
                    start_date: startDate,
                    end_date: endDate,
                    targets: [
                        {
                            equipment_id: target.equipment_id,
                            process_code: target.process_code,
                        },
                    ],
                };

                try {
                    const resp = await analyzeBatch(req);
                    if (resp.results && resp.results[target.equipment_id]) {
                        // Update Reactively
                        batchResults = {
                            ...batchResults,
                            [target.equipment_id]:
                                resp.results[target.equipment_id],
                        };
                    }
                } catch (err) {
                    console.error(
                        `Failed to analyze ${target.equipment_id}:`,
                        err,
                    );
                    // Mark as failed in UI? Or just leave empty
                }

                processedCount++;
                // Small delay to allow UI to breathe
                await new Promise((r) => setTimeout(r, 100));
            }
        } catch (e) {
            console.error("Sequential Analysis Fatal Error:", e);
            batchError = e.message || "Unknown error";
        } finally {
            batchLoading = false;
            batchAttempted = true;
        }
    }

    // Auto-trigger when rankings are loaded
    $: if (
        filteredRankings &&
        filteredRankings.length > 0 &&
        !batchAttempted &&
        !batchLoading &&
        !loading
    ) {
        console.log(
            "Reactive Trigger: Starting Sequential Analysis... Len:",
            filteredRankings.length,
        );
        runSequentialAnalysis(filteredRankings.slice(0, 20));
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

    async function handleIngest() {
        loading = true;
        try {
            const start = new Date(startDate + "T00:00:00Z").toISOString();
            const end = new Date(endDate + "T23:59:59Z").toISOString();
            await triggerIngest(start, end);
            await refreshMart();
            alert("Ingestion complete!");
            await loadRankings();
        } catch (e) {
            alert(e.message);
        } finally {
            loading = false;
        }
    }
</script>

<div class="p-6">
    <!-- Controls -->
    <div class="card bg-base-100 shadow-xl mb-6">
        <div class="card-body">
            <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                <label class="form-control w-full">
                    <div class="label">
                        <span class="label-text font-bold">Start Date</span>
                    </div>
                    <input
                        type="date"
                        bind:value={startDate}
                        class="input input-bordered w-full"
                    />
                </label>
                <label class="form-control w-full">
                    <div class="label">
                        <span class="label-text font-bold">End Date</span>
                    </div>
                    <input
                        type="date"
                        bind:value={endDate}
                        class="input input-bordered w-full"
                    />
                </label>
                <label class="form-control w-full">
                    <div class="label">
                        <span class="label-text font-bold">Defect Name</span>
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
                        Load
                    </button>
                    <button
                        class="btn btn-secondary flex-1"
                        on:click={handleIngest}
                        disabled={loading}
                    >
                        {#if loading && !selectedEquipment}<span
                                class="loading loading-spinner"
                            ></span>{/if}
                        Ingest
                    </button>
                </div>
            </div>

            <!-- Advanced Filters (Client Side) -->
            <div class="collapse collapse-arrow bg-base-200 mt-4 rounded-box">
                <input type="checkbox" />
                <div class="collapse-title text-md font-medium">
                    Advanced Filters
                </div>
                <div
                    class="collapse-content grid grid-cols-1 md:grid-cols-2 gap-4"
                >
                    <label class="form-control">
                        <div class="label">
                            <span class="label-text"
                                >Process Code (e.g. >1000, 100-200)</span
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
                            <span class="label-text">Equipment ID</span>
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
                            <th>Equipment</th>
                            <th>Process</th>
                            <th>Glass Count</th>
                            <th>Defect Rate</th>
                            <th>Overall</th>
                            <th>Delta</th>
                            <!-- <th>Action</th> -->
                        </tr>
                    </thead>
                    <tbody>
                        {#each filteredRankings as rank}
                            <tr
                                class:bg-base-200={selectedEquipment?.equipment_id ===
                                    rank.equipment_id}
                            >
                                <td class="font-bold">{rank.equipment_id}</td>
                                <td>{rank.process_code}</td>
                                <td>{rank.glass_count.toLocaleString()}</td>
                                <td>{rank.defect_rate.toFixed(3)}</td>
                                <td>{rank.overall_rate.toFixed(3)}</td>
                                <td
                                    class={rank.delta > 0
                                        ? "text-success font-bold"
                                        : "text-error font-bold"}
                                >
                                    {rank.delta > 0
                                        ? "+"
                                        : ""}{rank.delta.toFixed(3)}
                                </td>
                                <!-- No Action cell -->
                            </tr>
                        {:else}
                            <tr
                                ><td
                                    colspan="6"
                                    class="text-center py-4 text-gray-500"
                                    >No data available</td
                                ></tr
                            >
                        {/each}
                    </tbody>
                </table>
            </div>
        </div>
    </div>

    <!-- Batch Analysis Cards -->
    <div class="divider">Detailed Analysis (Top 20)</div>

    <!-- DEBUG UI -->
    <div class="alert alert-info text-xs mb-4 flex flex-col gap-2">
        <h3 class="font-bold underline">DEBUG CONSOLE</h3>
        <div class="grid grid-cols-2 gap-2">
            <div>Rankings (Orig): {rankings.length}</div>
            <div>
                Filtered (Active): {filteredRankings
                    ? filteredRankings.length
                    : 0}
            </div>
            <div>Batch Attempted: {batchAttempted}</div>
            <div>Batch Loading: {batchLoading}</div>
            <div class="text-error font-bold">
                Batch Error: {batchError || "None"}
            </div>
            <div>Current Defect: {defectName}</div>
        </div>

        <div class="bg-base-100 p-2 rounded border border-blue-300">
            <strong>Result Keys ({Object.keys(batchResults).length}):</strong>
            <br />
            <span class="break-all font-mono text-[10px]"
                >{Object.keys(batchResults).join(", ")}</span
            >
        </div>

        <div class="bg-base-100 p-2 rounded border border-blue-300">
            <strong>First Target ID:</strong>
            {rankings[0]?.equipment_id}
        </div>

        <div class="flex gap-2 mt-2">
            <button
                class="btn btn-xs btn-primary"
                on:click={() => runSequentialAnalysis(rankings.slice(0, 20))}
                >Retry Analysis</button
            >
            <button class="btn btn-xs btn-secondary" on:click={loadRankings}
                >Reload Rankings</button
            >
        </div>
    </div>

    {#if batchLoading}
        <center><span class="loading loading-spinner loading-lg"></span></center
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
