// Dimuat tanpa defer di <head> supaya tema terpasang sebelum halaman digambar (tidak berkedip).
(function () {
	'use strict';
	var stored = null;
	try {
		stored = window.localStorage.getItem('tocatdz-theme');
	} catch (e) {
		stored = null;
	}
	var theme = stored === 'dark' || stored === 'light'
		? stored
		: (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
	document.documentElement.setAttribute('data-bs-theme', theme);
})();
