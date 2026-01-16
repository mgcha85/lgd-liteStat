import { writable } from 'svelte/store';

// Check for stored theme or default to 'corporate'
const storedTheme = localStorage.getItem('theme') || 'corporate';

export const theme = writable(storedTheme);

// Subscribe to changes and update localStorage
theme.subscribe((value) => {
    localStorage.setItem('theme', value);
    document.documentElement.setAttribute('data-theme', value);
});
