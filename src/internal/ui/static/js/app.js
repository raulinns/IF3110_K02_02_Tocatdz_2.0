// Perilaku halaman. Semua aksi tetap berupa form biasa ke server; tanpa JS halaman tetap bisa dipakai.
(function () {
	'use strict';

	var root = document.documentElement;

	function storeTheme(theme) {
		try {
			window.localStorage.setItem('tocatdz-theme', theme);
		} catch (e) {
			// Penyimpanan diblokir browser; tema tetap berlaku sampai halaman dimuat ulang.
		}
	}

	function applyTheme(theme) {
		root.setAttribute('data-bs-theme', theme);
		document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
			var dark = theme === 'dark';
			button.setAttribute('aria-pressed', dark ? 'true' : 'false');
			button.setAttribute('title', dark ? 'Ganti ke mode terang' : 'Ganti ke mode gelap');
		});
	}

	function initTheme() {
		applyTheme(root.getAttribute('data-bs-theme') === 'dark' ? 'dark' : 'light');
		document.querySelectorAll('[data-theme-toggle]').forEach(function (button) {
			button.addEventListener('click', function () {
				var next = root.getAttribute('data-bs-theme') === 'dark' ? 'light' : 'dark';
				applyTheme(next);
				storeTheme(next);
			});
		});
	}

	// Sidebar disembunyikan/ditampilkan; di layar kecil sidebar default tertutup lewat CSS.
	function initSidebar() {
		var toggle = document.getElementById('sidebarCollapse');
		var sidebar = document.getElementById('sidebar');
		var body = document.getElementById('body');
		if (!toggle || !sidebar || !body) {
			return;
		}
		toggle.addEventListener('click', function () {
			var open = !sidebar.classList.contains('toggled');
			sidebar.classList.toggle('toggled', open);
			body.classList.toggle('toggled', open);
			toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
		});
	}

	// Tautan sidebar yang paling cocok dengan URL saat ini ditandai aktif.
	function markActiveLink() {
		var path = window.location.pathname;
		var best = null;
		var bestLength = -1;
		document.querySelectorAll('#sidebar a[data-nav]').forEach(function (link) {
			var prefix = link.getAttribute('data-nav');
			var matches = path === prefix || path.indexOf(prefix + '/') === 0;
			if (matches && prefix.length > bestLength) {
				best = link;
				bestLength = prefix.length;
			}
		});
		if (best) {
			best.classList.add('active');
			best.setAttribute('aria-current', 'page');
		}
	}

	// Form bertanda data-confirm meminta konfirmasi lewat modal sebelum dikirim.
	function initConfirm() {
		var modalElement = document.getElementById('confirmModal');
		if (!modalElement || !window.bootstrap || !window.bootstrap.Modal) {
			return;
		}
		var modal = window.bootstrap.Modal.getOrCreateInstance(modalElement);
		var message = modalElement.querySelector('[data-confirm-message]');
		var accept = modalElement.querySelector('[data-confirm-accept]');
		var pending = null;
		document.querySelectorAll('form[data-confirm]').forEach(function (form) {
			form.addEventListener('submit', function (event) {
				if (form.dataset.confirmed === 'true') {
					return;
				}
				event.preventDefault();
				pending = form;
				if (message) {
					message.textContent = form.getAttribute('data-confirm');
				}
				modal.show();
			});
		});
		if (accept) {
			accept.addEventListener('click', function () {
				if (!pending) {
					return;
				}
				var form = pending;
				pending = null;
				form.dataset.confirmed = 'true';
				accept.disabled = true;
				modal.hide();
				if (typeof form.requestSubmit === 'function') {
					form.requestSubmit();
				} else {
					form.submit();
				}
			});
		}
	}

	document.addEventListener('DOMContentLoaded', function () {
		initTheme();
		initSidebar();
		markActiveLink();
		initConfirm();
	});
})();
