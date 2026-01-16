<script>
    import { onMount, onDestroy } from "svelte";
    import * as echarts from "echarts";

    export let equipment;
    export let results;

    let uniqueId = Math.random().toString(36).substr(2, 9);

    // Chart instances
    let charts = [];

    // DOM Elements
    let glassDiv, lotDiv, dailyDiv, heatTargetDiv, heatOthersDiv;

    $: if (results) {
        // Use requestAnimationFrame to ensure DOM is ready
        setTimeout(initCharts, 0);
    }

    function initCharts() {
        if (!results) return;
        disposeCharts(); // Cleanup old charts

        // 1. Glass Scatter (Target vs Others) - Split into Top/Bottom
        if (glassDiv) {
            const chart = echarts.init(glassDiv);

            // Prepare Data
            const targetData = results.glass_results
                .filter((r) => r.group_type === "Target")
                .map((r) => [r.work_date, r.total_defects]);
            const othersData = results.glass_results
                .filter((r) => r.group_type === "Others")
                .map((r) => [r.work_date, r.total_defects]);

            // Date Boundaries
            const allDates = [
                ...new Set(
                    results.glass_results.map((r) => r.work_date.split("T")[0]),
                ),
            ].sort();
            const markLineData = allDates.map((date) => ({ xAxis: date }));

            chart.setOption({
                title: {
                    text: "글라스 불량",
                    left: "center",
                    textStyle: { fontSize: 10 },
                },
                grid: { top: "15%", bottom: "10%", left: 40, right: 10 },
                xAxis: {
                    type: "time",
                    splitLine: { show: false },
                    axisLabel: {
                        rotate: 45,
                        formatter: {
                            year: "{yyyy}",
                            month: "{yyyy}-{MM}-{dd}",
                            day: "{yyyy}-{MM}-{dd}",
                        },
                    },
                },
                yAxis: {
                    type: "value",
                    splitLine: { show: true },
                    name: "불량 수",
                },
                series: [
                    {
                        name: "대상",
                        type: "scatter",
                        data: targetData,
                        symbolSize: 4,
                        itemStyle: { color: "#e74c3c" },
                        markLine: {
                            symbol: ["none", "none"],
                            label: { show: false },
                            lineStyle: { color: "#ccc", type: "dashed" },
                            data: markLineData,
                        },
                    },
                    {
                        name: "그 외",
                        type: "scatter",
                        data: othersData,
                        symbolSize: 3,
                        itemStyle: { color: "#bdc3c7", opacity: 0.6 },
                    },
                ],
            });
            charts.push(chart);
        }

        // 2. Lot Scatter (Unchanged)
        if (lotDiv) {
            const chart = echarts.init(lotDiv);
            const targetData = (results.lot_results || [])
                .filter((r) => r.group_type === "Target")
                .map((r, i) => [i, r.avg_defects]);
            const othersData = (results.lot_results || [])
                .filter((r) => r.group_type === "Others")
                .map((r, i) => [i, r.avg_defects]);

            chart.setOption({
                title: {
                    text: "Lot 평균 불량",
                    left: "center",
                    textStyle: { fontSize: 12 },
                },
                grid: { top: 30, bottom: 20, left: 40, right: 10 },
                xAxis: { type: "value", show: false },
                yAxis: { type: "value" },
                series: [
                    {
                        name: "그 외",
                        type: "scatter",
                        data: othersData,
                        symbolSize: 4,
                        symbol: "rect",
                        itemStyle: { color: "#95a5a6" },
                    },
                    {
                        name: "대상",
                        type: "scatter",
                        data: targetData,
                        symbolSize: 6,
                        symbol: "rect",
                        itemStyle: { color: "#e74c3c" },
                    },
                ],
            });
            charts.push(chart);
        }

        // 3. Daily Trend - Remove Symbols
        if (dailyDiv) {
            const chart = echarts.init(dailyDiv);
            const targetData = (results.daily_results || [])
                .filter((r) => r.group_type === "Target")
                .map((r) => [r.work_date, r.avg_defects]);
            const othersData = (results.daily_results || [])
                .filter((r) => r.group_type === "Others")
                .map((r) => [r.work_date, r.avg_defects]);

            chart.setOption({
                title: {
                    text: "일별 추이",
                    left: "center",
                    textStyle: { fontSize: 12 },
                },
                grid: { top: 30, bottom: 20, left: 40, right: 10 },
                xAxis: {
                    type: "time",
                    splitLine: { show: false },
                    axisLabel: {
                        rotate: 45,
                        formatter: {
                            year: "{yyyy}",
                            month: "{yyyy}-{MM}-{dd}",
                            day: "{yyyy}-{MM}-{dd}",
                        },
                    },
                },
                yAxis: { type: "value" },
                series: [
                    {
                        name: "그 외",
                        type: "line",
                        data: othersData,
                        showSymbol: false, // Scatter removed
                        lineStyle: { color: "#95a5a6", width: 1 },
                    },
                    {
                        name: "대상",
                        type: "line",
                        data: targetData,
                        showSymbol: false, // Scatter removed
                        lineStyle: { color: "#e74c3c", width: 2 },
                    },
                ],
            });
            charts.push(chart);
        }

        // 4. Heatmaps
        const heatTargetData = (results.heatmap_results || []).filter(
            (h) => h.group_type === "Target",
        );
        const heatOthersData = (results.heatmap_results || []).filter(
            (h) => h.group_type === "Others",
        );

        // Calculate Global Max for consistent scale
        const maxTarget =
            Math.max(...heatTargetData.map((d) => d.defect_rate)) || 0;
        const maxOthers =
            Math.max(...heatOthersData.map((d) => d.defect_rate)) || 0;
        const globalMax = Math.max(maxTarget, maxOthers) || 1;

        renderHeatmap(
            heatTargetDiv,
            heatTargetData,
            "대상 맵",
            ["#fff", "#e74c3c"],
            globalMax,
        );
        renderHeatmap(
            heatOthersDiv,
            heatOthersData,
            "그 외 맵",
            ["#fff", "#e74c3c"],
            globalMax,
        );
    }

    function renderHeatmap(container, data, title, colorRange, maxValue) {
        if (!container || !data || data.length === 0) return;

        const chart = echarts.init(container);
        // Prepare data: [x, y, value]
        const xSet = new Set(data.map((d) => d.x));
        const ySet = new Set(data.map((d) => d.y));
        const xLabels = Array.from(xSet).sort();
        const yLabels = Array.from(ySet).sort();

        const seriesData = data.map((d) => {
            return [xLabels.indexOf(d.x), yLabels.indexOf(d.y), d.defect_rate];
        });

        chart.setOption({
            title: { text: title, left: "center", textStyle: { fontSize: 12 } },
            tooltip: { position: "top" },
            grid: { top: 30, bottom: 20, left: 30, right: 10 },
            xAxis: {
                type: "category",
                data: xLabels,
                show: true,
                axisLabel: { fontSize: 8 },
            },
            yAxis: {
                type: "category",
                data: yLabels,
                show: true,
                axisLabel: { fontSize: 8 },
            },
            visualMap: {
                min: 0,
                max: maxValue, // Use synced global max
                calculable: false,
                show: false,
                inRange: { color: colorRange },
            },
            series: [
                {
                    type: "heatmap",
                    data: seriesData,
                    itemStyle: {
                        borderColor: "#eee",
                        borderWidth: 1,
                    },
                },
            ],
        });
        charts.push(chart);
    }

    function disposeCharts() {
        charts.forEach((c) => c.dispose());
        charts = [];
    }

    onDestroy(() => {
        disposeCharts();
    });
</script>

<div class="card bg-base-100 shadow-md mb-4 border border-base-200">
    <div class="card-body p-4">
        <!-- Header -->
        <h3 class="font-bold text-lg flex items-center gap-2">
            {equipment.process_code} - {equipment.equipment_id}
        </h3>

        <!-- Content -->
        {#if results}
            <!-- Metrics Summary -->
            {#if results.metrics}
                <div class="flex gap-4 mb-2 text-sm bg-base-50 p-2 rounded">
                    <div>
                        전체: <span class="font-bold"
                            >{results.metrics.overall_defect_rate?.toFixed(
                                3,
                            )}</span
                        >
                    </div>
                    <div>
                        대상: <span class="font-bold text-error"
                            >{results.metrics.target_defect_rate?.toFixed(
                                3,
                            )}</span
                        >
                    </div>
                    <div>
                        그 외: <span class="font-bold"
                            >{results.metrics.others_defect_rate?.toFixed(
                                3,
                            )}</span
                        >
                    </div>
                    <div>
                        차이: <span
                            class={results.metrics.delta > 0
                                ? "text-success font-bold"
                                : "text-error font-bold"}
                            >{results.metrics.delta?.toFixed(3)}</span
                        >
                    </div>
                </div>
            {/if}

            <div class="grid grid-cols-4 gap-2 h-64">
                <!-- Col 1: Stacked Glass & Lot -->
                <div class="flex flex-col gap-1 h-full">
                    <!-- Glass Scatter -->
                    <div
                        bind:this={glassDiv}
                        class="flex-1 w-full border rounded"
                    ></div>
                    <!-- Lot Scatter -->
                    <div
                        bind:this={lotDiv}
                        class="h-24 w-full border rounded"
                    ></div>
                </div>

                <!-- Col 2: Daily Trend -->
                <div
                    bind:this={dailyDiv}
                    class="w-full h-full border rounded"
                ></div>

                <!-- Col 3: Heatmap Target -->
                <div
                    bind:this={heatTargetDiv}
                    class="w-full h-full border rounded"
                ></div>

                <!-- Col 4: Heatmap Others -->
                <div
                    bind:this={heatOthersDiv}
                    class="w-full h-full border rounded"
                ></div>
            </div>
        {:else}
            <div class="flex items-center justify-center h-64 text-gray-400">
                분석 중...
            </div>
        {/if}
    </div>
</div>
