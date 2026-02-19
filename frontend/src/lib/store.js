import { writable } from 'svelte/store';

const storedTheme = localStorage.getItem('theme') || 'corporate';
export const theme = writable(storedTheme);

theme.subscribe((value) => {
    localStorage.setItem('theme', value);
    document.documentElement.setAttribute('data-theme', value);
});

const storedChartMode = localStorage.getItem('chartMode') || 'image';
export const chartMode = writable(storedChartMode);

chartMode.subscribe((value) => {
    localStorage.setItem('chartMode', value);
});
