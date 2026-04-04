// NodeRouter - Minimal SSE Client + UI Handlers
(function() {
    'use strict';

    // Read config from data attributes on <body>
    var body = document.body;
    var refreshInt = parseInt(body.getAttribute('data-refresh-int'), 10) || 15;
    var btcClearnet = parseInt(body.getAttribute('data-btc-clear'), 10) || 0;
    var btcTor = parseInt(body.getAttribute('data-btc-tor'), 10) || 0;
    var btcI2p = parseInt(body.getAttribute('data-btc-i2p'), 10) || 0;

    var refreshStart = Date.now();
    var lineEl = document.getElementById('refreshLine');
    var lastEl = document.getElementById('lastUpdate');
    var intervalEl = document.getElementById('refreshInterval');

    // Set refresh interval display on load
    if (intervalEl) intervalEl.textContent = 'Updates every ' + refreshInt + 's';

    // Block timestamps for age calculation
    var blockTimestamps = [];

    // Drag-to-scroll for Bitcoin blockchain graphic
    var btcChainVisual = document.querySelector('[data-btc-blocks]');
    if (btcChainVisual) {
        var isDown = false, startX, scrollLeft;
        btcChainVisual.addEventListener('mousedown', function(e) {
            isDown = true;
            startX = e.pageX - btcChainVisual.offsetLeft;
            scrollLeft = btcChainVisual.scrollLeft;
        });
        btcChainVisual.addEventListener('mouseleave', function() { isDown = false; });
        btcChainVisual.addEventListener('mouseup', function() { isDown = false; });
        btcChainVisual.addEventListener('mousemove', function(e) {
            if (!isDown) return;
            e.preventDefault();
            var x = e.pageX - btcChainVisual.offsetLeft;
            var walk = (x - startX) * 1.5;
            btcChainVisual.scrollLeft = scrollLeft - walk;
        });
    }

    // Drag-to-scroll for Monero blockchain graphic
    var xmrChainVisual = document.querySelector('[data-xmr-blocks]');
    if (xmrChainVisual) {
        var xmrIsDown = false, xmrStartX, xmrScrollLeft;
        xmrChainVisual.addEventListener('mousedown', function(e) {
            xmrIsDown = true;
            xmrStartX = e.pageX - xmrChainVisual.offsetLeft;
            xmrScrollLeft = xmrChainVisual.scrollLeft;
        });
        xmrChainVisual.addEventListener('mouseleave', function() { xmrIsDown = false; });
        xmrChainVisual.addEventListener('mouseup', function() { xmrIsDown = false; });
        xmrChainVisual.addEventListener('mousemove', function(e) {
            if (!xmrIsDown) return;
            e.preventDefault();
            var x = e.pageX - xmrChainVisual.offsetLeft;
            var walk = (x - xmrStartX) * 1.5;
            xmrChainVisual.scrollLeft = xmrScrollLeft - walk;
        });
    }

    // Auto-update block ages every minute
    setInterval(function() {
        var ages = document.querySelectorAll('.block-age');
        var now = Math.floor(Date.now() / 1000);
        ages.forEach(function(el, i) {
            if (i < blockTimestamps.length && blockTimestamps[i] > 0) {
                var diff = now - blockTimestamps[i];
                var ageMins = Math.floor(diff / 60);
                if (ageMins < 1) {
                    el.textContent = 'Just now';
                } else if (ageMins < 60) {
                    el.textContent = ageMins + ' min ago';
                } else {
                    el.textContent = Math.floor(ageMins / 60) + 'h ago';
                }
            }
        });
    }, 60000);

    // Refresh progress bar animation
    function animLoop() {
        var p = Math.min((Date.now() - refreshStart) / (refreshInt * 1000), 1);
        if (lineEl) lineEl.style.width = (p * 100).toFixed(1) + '%';
        if (p < 1) requestAnimationFrame(animLoop);
    }
    requestAnimationFrame(animLoop);

    // SSE connection
    var evt = new EventSource('/sse/stream');
    evt.onmessage = function(e) {
        try {
            var d = JSON.parse(e.data);
            if (d.config_changed) { location.reload(); return; }
            if (d.bitcoin) updateBitcoin(d.bitcoin);
            if (d.bitcoin && d.bitcoin.recent_blocks) updateBlocks(d.bitcoin.recent_blocks);
            if (d.fulcrum) updateFulcrum(d.fulcrum);
            if (d.mempool) updateMempool(d.mempool);
            if (d.monero) updateMonero(d.monero);
            if (d.monero && d.monero.recent_blocks) updateXmrBlocks(d.monero.recent_blocks);
            if (d.last_successful_refresh) {
                var ts = new Date(d.last_successful_refresh * 1000).toLocaleTimeString();
                if (lastEl) lastEl.textContent = 'Updated ' + ts;
            }
            if (d.refresh_interval && d.refresh_interval !== refreshInt) {
                refreshInt = d.refresh_interval;
                if (intervalEl) intervalEl.textContent = 'Updates every ' + refreshInt + 's';
            }
            refreshStart = Date.now();
            requestAnimationFrame(animLoop);
        } catch(x) {}
    };
    evt.onerror = function() {
        setTimeout(function() { location.reload(); }, 5000);
    };

    // QR code overlay
    function showQR(el) {
        var addr = el.querySelector('.qr-address').textContent;
        var qrData = el.getAttribute('data-qr');
        var c = document.getElementById('qrContent');
        c.innerHTML = '<span class="qr-overlay-label">' + addr + '</span><img src="' + qrData + '" alt="QR">';
        document.getElementById('qrOvl').classList.add('visible');
        document.body.style.overflow = 'hidden';
    }
    window.showQR = showQR;

    function closeQR() {
        document.getElementById('qrOvl').classList.remove('visible');
        document.body.style.overflow = '';
    }
    window.closeQR = closeQR;

    // Peer overlay
    function closePeer() {
        document.getElementById('peerOvl').classList.remove('visible');
        document.body.style.overflow = '';
    }
    window.closePeer = closePeer;


    var qrOvl = document.getElementById('qrOvl');
    if (qrOvl) {
        qrOvl.addEventListener('click', function(e) {
            if (e.target === this) closeQR();
        });
    }

    var peerOvl = document.getElementById('peerOvl');
    if (peerOvl) {
        peerOvl.addEventListener('click', function(e) {
            if (e.target === this) closePeer();
        });
    }

    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') { closeQR(); closePeer(); closeXmrPeer(); }
    });

    // Peer topology donut chart
    function drawChart() {
        var cv = document.getElementById('peerChart');
        if (!cv) return;
        var ctx = cv.getContext('2d');
        var w = cv.width, h = cv.height;
        var cx = w / 2, cy = h / 2;
        var r = Math.min(w, h) / 2 - 10;
        var ir = r * 0.6;
        ctx.clearRect(0, 0, w, h);

        var cl = btcClearnet, tor = btcTor, i2p = btcI2p;
        var tot = cl + tor + i2p;

        if (tot === 0) {
            ctx.beginPath();
            ctx.arc(cx, cy, r, 0, 2 * Math.PI);
            ctx.arc(cx, cy, ir, 0, 2 * Math.PI, true);
            ctx.fillStyle = 'rgba(255,255,255,0.05)';
            ctx.fill();
            return;
        }

        var cols = { cl: '#ff9500', tor: '#af52de', i2p: '#636366' };
        var sa = -Math.PI / 2;

        [
            { v: cl, c: cols.cl },
            { v: tor, c: cols.tor },
            { v: i2p, c: cols.i2p }
        ].forEach(function(s) {
            if (s.v === 0) return;
            var sl = (s.v / tot) * 2 * Math.PI;
            ctx.beginPath();
            ctx.arc(cx, cy, r, sa, sa + sl);
            ctx.arc(cx, cy, ir, sa + sl, sa, true);
            ctx.closePath();
            ctx.fillStyle = s.c;
            ctx.fill();
            sa += sl;
        });
    }
    window.drawChart = drawChart;

    // Helper: set text content safely
    function setText(selector, val) {
        var el = document.querySelector('[' + selector + ']');
        if (el) el.textContent = val;
    }

    // Update Bitcoin module data
    function updateBitcoin(d) {
        // Status
        var statusDot = document.querySelector('[data-btc-status]');
        if (statusDot) {
            statusDot.className = 'status-dot ' + (d.connected ? 'ok' : 'err');
        }
        setText('data-btc-status-text', d.connected ? 'Connected' : d.error);
        // Sync
        setText('data-btc-sync', d.blocks + ' / ' + d.headers + ' (' + d.sync_progress.toFixed(2) + '%)');
        var syncBar = document.querySelector('[data-btc-sync-bar]');
        if (syncBar) {
            syncBar.style.width = d.sync_progress.toFixed(1) + '%';
            syncBar.className = 'sync-bar ' + syncClass(d.sync_progress);
        }
        // Stats
        setText('data-btc-size', formatBytes(d.size_on_disk));
        setText('data-btc-version', d.version);
        setText('data-btc-mempool', formatBytes(d.mempool_usage));
        setText('data-btc-mempool-sub', commaInt(d.mempool_size) + ' tx \u00B7 ' + d.mempool_pct.toFixed(2) + '% of ' + formatBytesDec(d.mempool_max));
        setText('data-btc-uptime', formatUptime(d.uptime));
        setText('data-btc-peers', d.connections);
        setText('data-btc-peers-sub', d.inbound_count + ' in / ' + d.outbound_count + ' out');
        // Update chart data
        btcClearnet = d.clearnet_count;
        btcTor = d.tor_count;
        btcI2p = d.i2p_count;
        // Update latency badge
        setText('data-btc-latency', d.latency_ms + 'ms');
    }

    // Cached block element references for fast updates
    var cachedBlockEls = [];

    // Update recent blocks - fast in-place updates
    function updateBlocks(blocks) {
        var chainVisual = document.querySelector('.chain-visual');
        if (!chainVisual) return;

        // Store timestamps for age calculation
        blockTimestamps = blocks.map(function(b) { return b.timestamp || 0; });

        var existingBlocks = chainVisual.querySelectorAll('.chain-block');
        var existingLinks = chainVisual.querySelectorAll('.chain-link');

        // If count changed, rebuild
        if (existingBlocks.length !== blocks.length) {
            rebuildBlocks(blocks);
            return;
        }

        // Fast in-place update using cached references or direct children
        var now = Math.floor(Date.now() / 1000);
        for (var i = 0; i < blocks.length; i++) {
            var b = blocks[i];
            var blockEl = cachedBlockEls[i] || existingBlocks[i];
            if (!blockEl) continue;

            // Cache reference if not already cached
            if (!cachedBlockEls[i]) cachedBlockEls[i] = blockEl;

            // Direct child access is faster than querySelector
            var children = blockEl.children;
            for (var j = 0; j < children.length; j++) {
                var child = children[j];
                var cls = child.className;
                if (cls === 'block-height') {
                    child.textContent = '#' + b.height;
                } else if (cls === 'block-size') {
                    child.textContent = b.size_mb.toFixed(3) + ' MB';
                } else if (cls === 'block-tx') {
                    child.textContent = commaInt(b.tx_count) + ' tx';
                } else if (cls === 'block-age') {
                    var diff = now - (b.timestamp || 0);
                    var ageMins = Math.floor(diff / 60);
                    if (ageMins < 1) child.textContent = 'Just now';
                    else if (ageMins < 60) child.textContent = ageMins + ' min ago';
                    else child.textContent = Math.floor(ageMins / 60) + 'h ago';
                }
            }

            // Remove new-block class after first update
            if (i === 0 && blockEl.classList.contains('new-block')) {
                blockEl.classList.remove('new-block');
            }
        }
    }

    function rebuildBlocks(blocks) {
        var chainVisual = document.querySelector('.chain-visual');
        if (!chainVisual) return;

        blockTimestamps = blocks.map(function(b) { return b.timestamp || 0; });
        cachedBlockEls = [];

        var now = Math.floor(Date.now() / 1000);
        var frag = document.createDocumentFragment();

        for (var i = 0; i < blocks.length; i++) {
            var b = blocks[i];
            var diff = now - (b.timestamp || 0);
            var ageMins = Math.floor(diff / 60);
            var ageStr;
            if (ageMins < 1) ageStr = 'Just now';
            else if (ageMins < 60) ageStr = ageMins + ' min ago';
            else ageStr = Math.floor(ageMins / 60) + 'h ago';

            if (i > 0) {
                var link = document.createElement('div');
                link.className = 'chain-link';
                frag.appendChild(link);
            }

            var block = document.createElement('div');
            block.className = 'chain-block' + (i === 0 ? ' new-block' : '');
            block.setAttribute('data-block-index', i);

            var hEl = document.createElement('div');
            hEl.className = 'block-height';
            hEl.setAttribute('data-block-height', i);
            hEl.textContent = '#' + b.height;
            block.appendChild(hEl);

            var sEl = document.createElement('div');
            sEl.className = 'block-size';
            sEl.setAttribute('data-block-size', i);
            sEl.textContent = b.size_mb.toFixed(3) + ' MB';
            block.appendChild(sEl);

            var tEl = document.createElement('div');
            tEl.className = 'block-tx';
            tEl.setAttribute('data-block-tx', i);
            tEl.textContent = commaInt(b.tx_count) + ' tx';
            block.appendChild(tEl);

            var aEl = document.createElement('div');
            aEl.className = 'block-age';
            aEl.setAttribute('data-block-age', i);
            aEl.textContent = ageStr;
            block.appendChild(aEl);

            frag.appendChild(block);
            cachedBlockEls[i] = block;
        }

        chainVisual.innerHTML = '';
        chainVisual.appendChild(frag);
    }

    // Update Fulcrum module data
    function updateFulcrum(d) {
        var statusDot = document.querySelector('[data-ful-status]');
        if (statusDot) {
            statusDot.className = 'status-dot ' + (d.connected ? 'ok' : 'err');
        }
        setText('data-ful-status-text', d.connected ? 'Connected' : d.error);
        setText('data-ful-version', d.version);
        // Update sync text and bar
        setText('data-ful-sync', d.header_height + ' / ' + (d.btc_headers || '--') + ' (' + d.sync_pct.toFixed(2) + '%)');
        var syncBar = document.querySelector('[data-ful-sync-bar]');
        if (syncBar) {
            syncBar.style.width = d.sync_pct.toFixed(1) + '%';
            syncBar.className = 'sync-bar ' + syncClass(d.sync_pct);
        }
        // Update latency badge
        setText('data-ful-latency', d.latency_ms + 'ms');
    }

    // Update Mempool module data
    function updateMempool(d) {
        var statusDot = document.querySelector('[data-mp-status]');
        if (statusDot) {
            statusDot.className = 'status-dot ' + (d.connected ? 'ok' : 'err');
        }
        setText('data-mp-status-text', d.connected ? 'Connected' : d.error);
        // Format fees with decimal if subsat enabled
        var feeFmt = d.subsat ? function(v) { return v.toFixed(1); } : function(v) { return Math.round(v); };
        setText('data-mp-fee-economy', feeFmt(d.economy));
        setText('data-mp-fee-hour', feeFmt(d.hour));
        setText('data-mp-fee-halfhour', feeFmt(d.half_hour));
        // Fastest fee: round up unless under 1 sat/vB
        var fastestVal;
        if (d.fastest < 1.0) {
            fastestVal = d.subsat ? d.fastest.toFixed(1) : Math.round(d.fastest);
        } else {
            fastestVal = Math.ceil(d.fastest);
        }
        setText('data-mp-fee-fastest', fastestVal);
        setText('data-mp-epoch-pct', d.epoch_pct ? d.epoch_pct.toFixed(1) + '%' : '--');
        setText('data-mp-epoch-chg', d.epoch_chg ? (d.epoch_chg > 0 ? '+' : '') + d.epoch_chg.toFixed(2) + '%' : '--');
        setText('data-mp-epoch-left', d.epoch_left ? commaInt(d.epoch_left) : '--');
        // Update latency badge
        setText('data-mp-latency', d.latency_ms + 'ms');
    }

    // Update Monero module data
    function updateMonero(d) {
        var statusDot = document.querySelector('[data-xmr-status]');
        if (statusDot) {
            statusDot.className = 'status-dot ' + (d.connected ? 'ok' : 'err');
        }
        setText('data-xmr-status-text', d.connected ? 'Connected' : d.error);
        setText('data-xmr-sync', d.height + ' / ' + d.target_height + ' (' + d.sync_progress.toFixed(2) + '%)');
        var syncBar = document.querySelector('[data-xmr-sync-bar]');
        if (syncBar) {
            syncBar.style.width = d.sync_progress.toFixed(1) + '%';
            syncBar.className = 'sync-bar ' + syncClass(d.sync_progress);
        }
        setText('data-xmr-nettype', d.net_type);
        setText('data-xmr-dbsize', formatBytes(d.db_size));
        setText('data-xmr-txpool', commaInt(d.tx_pool));
        setText('data-xmr-diff', formatDifficulty(d.difficulty));
        setText('data-xmr-txcount', commaInt(d.tx_count));
        setText('data-xmr-peers', d.connections);
        setText('data-xmr-peers-sub', d.in_peers + ' in / ' + d.out_peers + ' out');
        // Update Monero peer chart
        xmrInPeers = d.in_peers;
        xmrOutPeers = d.out_peers;
        // Update latency badge
        setText('data-xmr-latency', d.latency_ms + 'ms');
    }

    // Monero peer chart
    var xmrInPeers = 0, xmrOutPeers = 0;
    function drawXmrChart() {
        var cv = document.getElementById('xmrPeerChart');
        if (!cv) return;
        var ctx = cv.getContext('2d');
        var w = cv.width, h = cv.height;
        var cx = w / 2, cy = h / 2;
        var r = Math.min(w, h) / 2 - 10;
        var ir = r * 0.6;
        ctx.clearRect(0, 0, w, h);

        var inp = xmrInPeers, outp = xmrOutPeers;
        var tot = inp + outp;

        if (tot === 0) {
            ctx.beginPath();
            ctx.arc(cx, cy, r, 0, 2 * Math.PI);
            ctx.arc(cx, cy, ir, 0, 2 * Math.PI, true);
            ctx.fillStyle = 'rgba(255,255,255,0.05)';
            ctx.fill();
            return;
        }

        var cols = { inp: '#ff9500', outp: '#0a84ff' };
        var sa = -Math.PI / 2;

        [
            { v: inp, c: cols.inp },
            { v: outp, c: cols.outp }
        ].forEach(function(s) {
            if (s.v === 0) return;
            var sl = (s.v / tot) * 2 * Math.PI;
            ctx.beginPath();
            ctx.arc(cx, cy, r, sa, sa + sl);
            ctx.arc(cx, cy, ir, sa + sl, sa, true);
            ctx.closePath();
            ctx.fillStyle = s.c;
            ctx.fill();
            sa += sl;
        });
    }

    // Monero peer overlay
    function closeXmrPeer() {
        document.getElementById('xmrPeerOvl').classList.remove('visible');
        document.body.style.overflow = '';
    }
    window.closeXmrPeer = closeXmrPeer;

    var xmrPeerOvl = document.getElementById('xmrPeerOvl');
    if (xmrPeerOvl) {
        xmrPeerOvl.addEventListener('click', function(e) {
            if (e.target === this) closeXmrPeer();
        });
    }

    // Update Monero recent blocks
    var xmrBlockTimestamps = [];
    var xmrCachedBlockEls = [];

    function updateXmrBlocks(blocks) {
        var chainVisual = document.querySelector('[data-xmr-blocks]');
        if (!chainVisual) return;

        xmrBlockTimestamps = blocks.map(function(b) { return b.timestamp || 0; });
        var existingBlocks = chainVisual.querySelectorAll('.chain-block');

        if (existingBlocks.length !== blocks.length) {
            rebuildXmrBlocks(blocks);
            return;
        }

        var now = Math.floor(Date.now() / 1000);
        for (var i = 0; i < blocks.length; i++) {
            var b = blocks[i];
            var blockEl = xmrCachedBlockEls[i] || existingBlocks[i];
            if (!blockEl) continue;
            if (!xmrCachedBlockEls[i]) xmrCachedBlockEls[i] = blockEl;

            var children = blockEl.children;
            for (var j = 0; j < children.length; j++) {
                var child = children[j];
                var cls = child.className;
                if (cls === 'block-height') {
                    child.textContent = '#' + b.height;
                } else if (cls === 'block-size') {
                    child.textContent = b.size_mb.toFixed(3) + ' MB';
                } else if (cls === 'block-tx') {
                    child.textContent = commaInt(b.tx_count) + ' tx';
                } else if (cls === 'block-age') {
                    var diff = now - (b.timestamp || 0);
                    var ageMins = Math.floor(diff / 60);
                    if (ageMins < 1) child.textContent = 'Just now';
                    else if (ageMins < 60) child.textContent = ageMins + ' min ago';
                    else child.textContent = Math.floor(ageMins / 60) + 'h ago';
                }
            }
            if (i === 0 && blockEl.classList.contains('new-block')) {
                blockEl.classList.remove('new-block');
            }
        }
    }

    function rebuildXmrBlocks(blocks) {
        var chainVisual = document.querySelector('[data-xmr-blocks]');
        if (!chainVisual) return;

        xmrBlockTimestamps = blocks.map(function(b) { return b.timestamp || 0; });
        xmrCachedBlockEls = [];

        var now = Math.floor(Date.now() / 1000);
        var frag = document.createDocumentFragment();

        for (var i = 0; i < blocks.length; i++) {
            var b = blocks[i];
            var diff = now - (b.timestamp || 0);
            var ageMins = Math.floor(diff / 60);
            var ageStr;
            if (ageMins < 1) ageStr = 'Just now';
            else if (ageMins < 60) ageStr = ageMins + ' min ago';
            else ageStr = Math.floor(ageMins / 60) + 'h ago';

            if (i > 0) {
                var link = document.createElement('div');
                link.className = 'chain-link';
                frag.appendChild(link);
            }

            var block = document.createElement('div');
            block.className = 'chain-block' + (i === 0 ? ' new-block' : '');
            block.setAttribute('data-xmr-block-index', i);

            var hEl = document.createElement('div');
            hEl.className = 'block-height';
            hEl.setAttribute('data-xmr-block-height', i);
            hEl.textContent = '#' + b.height;
            block.appendChild(hEl);

            var sEl = document.createElement('div');
            sEl.className = 'block-size';
            sEl.setAttribute('data-xmr-block-size', i);
            sEl.textContent = b.size_mb.toFixed(1) + ' MB';
            block.appendChild(sEl);

            var tEl = document.createElement('div');
            tEl.className = 'block-tx';
            tEl.setAttribute('data-xmr-block-tx', i);
            tEl.textContent = commaInt(b.tx_count) + ' tx';
            block.appendChild(tEl);

            var aEl = document.createElement('div');
            aEl.className = 'block-age';
            aEl.setAttribute('data-xmr-block-age', i);
            aEl.textContent = ageStr;
            block.appendChild(aEl);

            frag.appendChild(block);
            xmrCachedBlockEls[i] = block;
        }

        chainVisual.innerHTML = '';
        chainVisual.appendChild(frag);
    }

    function formatDifficulty(d) {
        if (!d || d === 0) return '--';
        if (d >= 1e12) return (d / 1e12).toFixed(2) + 'T';
        if (d >= 1e9) return (d / 1e9).toFixed(2) + 'G';
        if (d >= 1e6) return (d / 1e6).toFixed(2) + 'M';
        return d.toFixed(0);
    }

    // Format helpers for JS updates
    function formatBytes(b) {
        if (b === 0) return '0 B';
        var k = 1024, sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        var i = Math.floor(Math.log(b) / Math.log(k));
        return (b / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i];
    }
    function formatBytesDec(b) {
        if (b === 0) return '0 B';
        var k = 1000, sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        var i = Math.floor(Math.log(b) / Math.log(k));
        return (b / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i];
    }
    function commaInt(n) {
        return n.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',');
    }
    function formatUptime(s) {
        if (s === 0) return '--';
        var d = Math.floor(s / 86400);
        var h = Math.floor((s % 86400) / 3600);
        var m = Math.floor((s % 3600) / 60);
        if (d > 0) return d + 'd ' + h + 'h ' + m + 'm';
        if (h > 0) return h + 'h ' + m + 'm';
        return m + 'm';
    }
    function syncClass(pct) {
        if (pct < 80) return 'sync-low';
        if (pct < 95) return 'sync-mid';
        return 'sync-high';
    }

    // Module toggle (expand/collapse)
    function toggle(hdrId, bodyId, btnId) {
        var b = document.getElementById(bodyId);
        var btn = document.getElementById(btnId);
        if (!b || !btn) return;
        b.classList.toggle('collapsed');
        btn.classList.toggle('open');
    }
    window.toggle = toggle;

    // Re-bind module toggle buttons after DOM update
    function bindModuleButtons() {
        var fulBtn = document.getElementById('fulBtn');
        if (fulBtn) {
            fulBtn.replaceWith(fulBtn.cloneNode(true));
            document.getElementById('fulBtn').addEventListener('click', function() { toggle('fulHdr', 'fulBody', 'fulBtn'); });
        }
        var mpBtn = document.getElementById('mpBtn');
        if (mpBtn) {
            mpBtn.replaceWith(mpBtn.cloneNode(true));
            document.getElementById('mpBtn').addEventListener('click', function() { toggle('mpHdr', 'mpBody', 'mpBtn'); });
        }
        var xmrBtn = document.getElementById('xmrBtn');
        if (xmrBtn) {
            xmrBtn.replaceWith(xmrBtn.cloneNode(true));
            document.getElementById('xmrBtn').addEventListener('click', function() { toggle('xmrHdr', 'xmrBody', 'xmrBtn'); });
        }
        // Re-bind peers card
        var peersCard = document.getElementById('peersCard');
        if (peersCard) {
            peersCard.replaceWith(peersCard.cloneNode(true));
            document.getElementById('peersCard').addEventListener('click', function() {
                document.getElementById('peerOvl').classList.add('visible');
                document.body.style.overflow = 'hidden';
                drawChart();
            });
        }
        // Re-bind Monero peers card
        var xmrPeersCard = document.getElementById('xmrPeersCard');
        if (xmrPeersCard) {
            xmrPeersCard.replaceWith(xmrPeersCard.cloneNode(true));
            document.getElementById('xmrPeersCard').addEventListener('click', function() {
                document.getElementById('xmrPeerOvl').classList.add('visible');
                document.body.style.overflow = 'hidden';
                drawXmrChart();
            });
        }
    }
    window.bindModuleButtons = bindModuleButtons;

    // Initial button binding
    bindModuleButtons();

    // PWA Service Worker registration
    if ('serviceWorker' in navigator) {
        navigator.serviceWorker.register('/static/sw.js').catch(function() {});
    }

})();
