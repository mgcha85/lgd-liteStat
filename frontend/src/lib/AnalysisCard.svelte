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

        // 1. Glass Scatter (Target vs Others)
        if (glassDiv) {
            const chart = echarts.init(glassDiv);
            const targetData = results.glass_results
                .filter((r) => r.group_type === "Target")
                .map((r) => [r.work_date, r.total_defects]);
            const othersData = results.glass_results
                .filter((r) => r.group_type === "Others")
                .map((r) => [r.work_date, r.total_defects]);

            chart.setOption({
                title: {
                    text: "Glass Defects",
                    left: "center",
                    textStyle: { fontSize: 12 },
                },
                grid: { top: 30, bottom: 20, left: 40, right: 10 },
                xAxis: { type: "time", splitLine: { show: false } },
                yAxis: { type: "value", splitLine: { show: true } },
                series: [
                    {
                        name: "Others",
                        type: "scatter",
                        data: othersData,
                        symbolSize: 3,
                        itemStyle: { color: "#bdc3c7", opacity: 0.6 },
                        large: true, // Optimize for large data
                    },
                    {
                        name: "Target",
                        type: "scatter",
                        data: targetData,
                        symbolSize: 5,
                        itemStyle: { color: "#e74c3c" },
                        large: true, // Optimize for large data
                    },
                ],
            });
            charts.push(chart);
        }

        // 2. Lot Scatter
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
                    text: "Lot Avg Defects",
                    left: "center",
                    textStyle: { fontSize: 12 },
                },
                grid: { top: 30, bottom: 20, left: 40, right: 10 },
                xAxis: { type: "value", show: false },
                yAxis: { type: "value" },
                series: [
                    {
                        name: "Others",
                        type: "scatter",
                        data: othersData,
                        symbolSize: 4,
                        symbol: "rect",
                        itemStyle: { color: "#95a5a6" },
                    },
                    {
                        name: "Target",
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

        // 3. Daily Trend
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
                    text: "Daily Trend",
                    left: "center",
                    textStyle: { fontSize: 12 },
                },
                grid: { top: 30, bottom: 20, left: 40, right: 10 },
                xAxis: { type: "time" },
                yAxis: { type: "value" },
                series: [
                    {
                        name: "Others",
                        type: "line",
                        data: othersData,
                        showSymbol: false,
                        lineStyle: { color: "#95a5a6", width: 1 },
                    },
                    {
                        name: "Target",
                        type: "line",
                        data: targetData,
                        showSymbol: true,
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
            "Target Map",
            ["#fff", "#e74c3c"],
            globalMax,
        );
        renderHeatmap(
            heatOthersDiv,
            heatOthersData,
            "Others Map",
            ["#fff", "#7f8c8d"],
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
            xAxis: { type: "category", data: xLabels, show: false },
            yAxis: { type: "category", data: yLabels, show: false },
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
            {equipment.equipment_id}
            <span class="badge badge-neutral">{equipment.process_code}</span>
        </h3>

        <!-- Content -->
        {#if results}
            <!-- Metrics Summary -->
            {#if results.metrics}
                <div class="flex gap-4 mb-2 text-sm bg-base-50 p-2 rounded">
                    <div>
                        Overall: <span class="font-bold"
                            >{results.metrics.overall_defect_rate?.toFixed(
                                3,
                            )}</span
                        >
                    </div>
                    <div>
                        Target: <span class="font-bold text-error"
                            >{results.metrics.target_defect_rate?.toFixed(
                                3,
                            )}</span
                        >
                    </div>
                    <div>
                        Others: <span class="font-bold"
                            >{results.metrics.others_defect_rate?.toFixed(
                                3,
                            )}</span
                        >
                    </div>
                    <div>
                        Delta: <span
                            class={results.metrics.delta > 0
                                ? "text-success font-bold"
                                : "text-error font-bold"}
                            >{results.metrics.delta?.toFixed(3)}</span
                        >
                    </div>
                </div>
            {/if}

            <div class="grid grid-cols-5 gap-2 h-64">
                <!-- 1. Glass Scatter -->
                <div
                    bind:this={glassDiv}
                    class="w-full h-full border rounded"
                ></div>

                <!-- 2. Lot Scatter -->
                <div
                    bind:this={lotDiv}
                    class="w-full h-full border rounded"
                ></div>

                <!-- 3. Daily Trend -->
                <div
                    bind:this={dailyDiv}
                    class="w-full h-full border rounded"
                ></div>

                <!-- 4. Heatmap Target -->
                <div
                    bind:this={heatTargetDiv}
                    class="w-full h-full border rounded"
                ></div>

                <!-- 5. Heatmap Others -->
                <div
                    bind:this={heatOthersDiv}
                    class="w-full h-full border rounded"
                ></div>
            </div>
        {:else}
            <div class="flex items-center justify-center h-64 text-gray-400">
                Waiting for analysis...
            </div>
        {/if}
    </div>
</div>
