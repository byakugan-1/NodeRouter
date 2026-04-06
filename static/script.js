// NodeRouter - Minimal SSE Client + UI Handlers
(function() {
    'use strict';

    var body = document.body;
    var refreshInt = parseInt(body.getAttribute('data-refresh-int'), 10) || 15;
    var refreshStart = parseInt(body.getAttribute('data-refresh-start'), 10) || Math.floor(Date.now() / 1000) - refreshInt;
    // Convert server timestamp (seconds) to milliseconds and adjust for current time
    var serverRefreshStart = refreshStart * 1000;
    var currentTime = Date.now();
    var elapsed = currentTime - serverRefreshStart;
    var remaining = (refreshInt * 1000) - elapsed;
    // If remaining is negative, we're in a new cycle
    if (remaining < 0) {
        remaining = refreshInt * 1000 + remaining;
    }
    var refreshStart = currentTime - (refreshInt * 1000 - remaining);
    
    var btcClearnet = parseInt(body.getAttribute('data-btc-clear'), 10) || 0;
    var btcTor = parseInt(body.getAttribute('data-btc-tor'), 10) || 0;
    var btcI2p = parseInt(body.getAttribute('data-btc-i2p'), 10) || 0;
    var lineEl = document.getElementById('refreshLine');
    var lastEl = document.getElementById('lastUpdate');
    var intervalEl = document.getElementById('refreshInterval');

    if (intervalEl) intervalEl.textContent = 'Updates every ' + refreshInt + 's';

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
        var elapsed = Date.now() - refreshStart;
        var p = Math.min(elapsed / (refreshInt * 1000), 1);
        if (lineEl) lineEl.style.width = (p * 100).toFixed(1) + '%';
        if (p < 1) requestAnimationFrame(animLoop);
    }
    requestAnimationFrame(animLoop);

    // SSE connection
    var evt = new EventSource('/sse/stream');
    evt.onmessage = function(e) {
        try {
            var d = JSON.parse(e.data);
            // NEVER reload the page - update elements individually
            if (d.config_changed) {
                // Config changed - update UI elements without page reload
                if (d.bitcoin) updateBitcoin(d.bitcoin);
                if (d.fulcrum) updateFulcrum(d.fulcrum);
                if (d.mempool) updateMempool(d.mempool);
                if (d.monero) updateMonero(d.monero);
                return;
            }
            // Only update if data exists to prevent flash
            if (d.bitcoin !== undefined) {
                if (d.bitcoin) updateBitcoin(d.bitcoin);
                if (d.bitcoin.recent_blocks) updateBlocks(d.bitcoin.recent_blocks);
            }
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
            // Update notification indicator
            if (typeof d.notif_enabled === 'boolean') {
                var mpNotif = document.getElementById('mpNotifIndicator');
                if (mpNotif) {
                    if (d.notif_enabled) mpNotif.classList.add('visible');
                    else mpNotif.classList.remove('visible');
                }
            }
            refreshStart = Date.now();
            requestAnimationFrame(animLoop);
        } catch(x) {
            console.error('[SSE] Parse error:', x);
        }
    };
    evt.onerror = function() {
        setTimeout(function() { location.reload(); }, 5000);
    };

    // Helper: set text content safely
    function setText(selector, val) {
        var el = document.querySelector('[' + selector + ']');
        if (el) el.textContent = val;
    }

    // Update Bitcoin module data
    function updateBitcoin(d) {
        var statusDot = document.querySelector('[data-btc-status]');
        if (statusDot) {
            statusDot.className = 'status-dot ' + (d.connected ? 'ok' : 'err');
        }
        setText('data-btc-status-text', d.connected ? 'Connected' : d.error);
        setText('data-btc-sync', d.blocks + ' / ' + d.headers + ' (' + d.sync_progress.toFixed(2) + '%)');
        var syncBar = document.querySelector('[data-btc-sync-bar]');
        if (syncBar) {
            syncBar.style.width = d.sync_progress.toFixed(1) + '%';
            syncBar.className = 'sync-bar ' + syncClass(d.sync_progress);
        }
        setText('data-btc-size', formatBytes(d.size_on_disk));
        setText('data-btc-version', d.version);
        setText('data-btc-mempool', formatBytes(d.mempool_usage));
        setText('data-btc-mempool-sub', commaInt(d.mempool_size) + ' tx \u00B7 ' + d.mempool_pct.toFixed(2) + '% of ' + formatBytesDec(d.mempool_max));
        setText('data-btc-uptime', formatUptime(d.uptime));
        setText('data-btc-peers', d.connections);
        setText('data-btc-peers-sub', d.inbound_count + ' in / ' + d.outbound_count + ' out');
        btcClearnet = d.clearnet_count;
        btcTor = d.tor_count;
        btcI2p = d.i2p_count;
        setText('data-btc-latency', d.latency_ms + 'ms');
    }

    // Cached block element references
    var cachedBlockEls = [];

    function updateBlocks(blocks) {
        var chainVisual = document.querySelector('.chain-visual');
        if (!chainVisual) return;

        blockTimestamps = blocks.map(function(b) { return b.timestamp || 0; });

        var existingBlocks = chainVisual.querySelectorAll('.chain-block');
        if (existingBlocks.length !== blocks.length) {
            rebuildBlocks(blocks);
            return;
        }

        var now = Math.floor(Date.now() / 1000);
        for (var i = 0; i < blocks.length; i++) {
            var b = blocks[i];
            var blockEl = cachedBlockEls[i] || existingBlocks[i];
            if (!blockEl) continue;
            if (!cachedBlockEls[i]) cachedBlockEls[i] = blockEl;

            var children = blockEl.children;
            for (var j = 0; j < children.length; j++) {
                var child = children[j];
                var cls = child.className;
                if (cls === 'block-height') {
                    var newHeight = '#' + b.height;
                    if (child.textContent !== newHeight) child.textContent = newHeight;
                } else if (cls === 'block-size') {
                    var newSize = b.size_mb.toFixed(3) + ' MB';
                    if (child.textContent !== newSize) child.textContent = newSize;
                } else if (cls === 'block-tx') {
                    var newTx = commaInt(b.tx_count) + ' tx';
                    if (child.textContent !== newTx) child.textContent = newTx;
                } else if (cls === 'block-age') {
                    var diff = now - (b.timestamp || 0);
                    var ageMins = Math.floor(diff / 60);
                    var newAge;
                    if (ageMins < 1) newAge = 'Just now';
                    else if (ageMins < 60) newAge = ageMins + ' min ago';
                    else newAge = Math.floor(ageMins / 60) + 'h ago';
                    if (child.textContent !== newAge) child.textContent = newAge;
                }
            }

            // Only remove new-block class if height changed
            if (i === 0 && blockEl.classList.contains('new-block')) {
                var heightEl = blockEl.querySelector('.block-height');
                if (heightEl && heightEl.textContent === '#' + b.height) {
                    // Keep new-block class for a bit, then remove
                    (function(el) {
                        setTimeout(function() { el.classList.remove('new-block'); }, 2000);
                    })(blockEl);
                }
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
        setText('data-ful-sync', d.header_height + ' / ' + (d.btc_headers || '--') + ' (' + d.sync_pct.toFixed(2) + '%)');
        var syncBar = document.querySelector('[data-ful-sync-bar]');
        if (syncBar) {
            syncBar.style.width = d.sync_pct.toFixed(1) + '%';
            syncBar.className = 'sync-bar ' + syncClass(d.sync_pct);
        }
        setText('data-ful-latency', d.latency_ms + 'ms');
    }

    // Update Mempool module data
    function updateMempool(d) {
        var statusDot = document.querySelector('[data-mp-status]');
        if (statusDot) {
            statusDot.className = 'status-dot ' + (d.connected ? 'ok' : 'err');
        }
        setText('data-mp-status-text', d.connected ? 'Connected' : d.error);
        var feeFmt = d.subsat ? function(v) { return v.toFixed(1); } : function(v) { return Math.round(v); };
        setText('data-mp-fee-economy', feeFmt(d.economy));
        setText('data-mp-fee-hour', feeFmt(d.hour));
        setText('data-mp-fee-halfhour', feeFmt(d.half_hour));
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
        setText('data-xmr-dbsize', formatBytes(d.db_size));
        setText('data-xmr-txpool', commaInt(d.tx_pool));
        setText('data-xmr-diff', formatDifficulty(d.difficulty));
        setText('data-xmr-txcount', commaInt(d.tx_count));
        setText('data-xmr-peers', d.connections);
        setText('data-xmr-peers-sub', d.in_peers + ' in / ' + d.out_peers + ' out');
        setText('data-xmr-latency', d.latency_ms + 'ms');
    }

    // Monero block handling
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

    // Module toggle
    function toggle(hdrId, bodyId, btnId) {
        var b = document.getElementById(bodyId);
        var btn = document.getElementById(btnId);
        if (!b || !btn) return;
        b.classList.toggle('collapsed');
        btn.classList.toggle('open');
    }
    window.toggle = toggle;

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
        var peersCard = document.getElementById('peersCard');
        if (peersCard) {
            peersCard.replaceWith(peersCard.cloneNode(true));
            document.getElementById('peersCard').addEventListener('click', function() {
                document.getElementById('peerOvl').classList.add('visible');
                document.body.style.overflow = 'hidden';
                drawChart();
            });
        }
    }
    window.bindModuleButtons = bindModuleButtons;
    bindModuleButtons();

    // Peer overlay
    function closePeer() {
        document.getElementById('peerOvl').classList.remove('visible');
        document.body.style.overflow = '';
    }
    window.closePeer = closePeer;

    function closeXmrPeer() {
        document.getElementById('xmrPeerOvl').classList.remove('visible');
        document.body.style.overflow = '';
    }
    window.closeXmrPeer = closeXmrPeer;

    var peerOvl = document.getElementById('peerOvl');
    if (peerOvl) {
        peerOvl.addEventListener('click', function(e) {
            if (e.target === this) closePeer();
        });
    }

    var xmrPeerOvl = document.getElementById('xmrPeerOvl');
    if (xmrPeerOvl) {
        xmrPeerOvl.addEventListener('click', function(e) {
            if (e.target === this) closeXmrPeer();
        });
    }

    document.addEventListener('keydown', function(e) {
        if (e.key === 'Escape') { closePeer(); closeXmrPeer(); window.closeNotifSettings(); }
    });

    // Close button for notification settings
    var notifCloseBtn = document.getElementById('notifCloseBtn');
    if (notifCloseBtn) {
        notifCloseBtn.addEventListener('click', function(e) {
            e.stopPropagation();
            window.closeNotifSettings();
        });
    }

    // Peer chart
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

    // ==================== Notification Settings ====================

    var notifOvl = document.getElementById('notifOvl');
    var settingsLink = document.getElementById('settingsLink');
    var mpNotifIndicator = document.getElementById('mpNotifIndicator');
    var notifContent = document.getElementById('notifContent');
    var notifEnabledToggle = document.getElementById('notifEnabled');

    if (settingsLink) {
        settingsLink.addEventListener('click', function(e) {
            e.preventDefault();
            openNotifSettings();
        });
    }

    function openNotifSettings() {
        if (!notifOvl) return;
        notifOvl.classList.add('visible');
        document.body.style.overflow = 'hidden';
        loadNotifSettings();
    }

    window.closeNotifSettings = function() {
        if (!notifOvl) return;
        notifOvl.classList.remove('visible');
        document.body.style.overflow = '';
        // Clear result messages
        var testResult = document.getElementById('notifTestResult');
        if (testResult) testResult.className = 'notif-result';
        var saveResult = document.getElementById('notifSaveResult');
        if (saveResult) saveResult.className = 'notif-result';
    };

    if (notifOvl) {
        notifOvl.addEventListener('click', function(e) {
            // Only close if clicking the overlay background, not the panel
            if (e.target === notifOvl && !e.target.closest('.overlay-panel')) {
                closeNotifSettings();
            }
        });
    }

    // Toggle content visibility based on master toggle
    if (notifEnabledToggle) {
        notifEnabledToggle.addEventListener('change', function() {
            if (this.checked) {
                notifContent.classList.add('visible');
            } else {
                notifContent.classList.remove('visible');
            }
        });
    }

    function loadNotifSettings() {
        fetch('/api/notifications')
            .then(function(r) {
                if (!r.ok) throw new Error('Network error');
                return r.json();
            })
            .then(function(data) {
                // Global settings
                var refreshIntervalEl = document.getElementById('refreshInterval');
                var showLatencyEl = document.getElementById('showLatency');
                var btcBlocksEl = document.getElementById('btcBlocksCount');
                var xmrBlocksEl = document.getElementById('xmrBlocksCount');
                if (refreshIntervalEl) refreshIntervalEl.value = data.refresh_interval || 10;
                if (showLatencyEl) showLatencyEl.checked = data.show_latency !== undefined ? data.show_latency : true;
                if (btcBlocksEl) btcBlocksEl.value = data.btc_blocks_count || 30;
                if (xmrBlocksEl) xmrBlocksEl.value = data.xmr_blocks_count || 15;

                // Service connections
                var svcMpEl = document.getElementById('svcMpEnabled');
                var svcFulEl = document.getElementById('svcFulEnabled');
                var svcXmrEl = document.getElementById('svcXmrEnabled');
                var connBtcRpc = document.getElementById('connBtcRpc');
                var connBtcUser = document.getElementById('connBtcUser');
                var connBtcPass = document.getElementById('connBtcPass');
                var connMpApi = document.getElementById('connMpApi');
                var connFulcrum = document.getElementById('connFulcrum');
                var connMonero = document.getElementById('connMonero');

                if (svcMpEl) svcMpEl.checked = data.svc_mp_enabled !== undefined ? data.svc_mp_enabled : true;
                if (svcFulEl) svcFulEl.checked = data.svc_ful_enabled !== undefined ? data.svc_ful_enabled : true;
                if (svcXmrEl) svcXmrEl.checked = data.svc_xmr_enabled !== undefined ? data.svc_xmr_enabled : true;
                if (connBtcRpc) connBtcRpc.value = data.conn_btc_rpc || '';
                if (connBtcUser) connBtcUser.value = data.conn_btc_user || '';
                if (connBtcPass) connBtcPass.value = data.conn_btc_pass || '';
                if (connMpApi) connMpApi.value = data.conn_mp_api || '';
                if (connFulcrum) connFulcrum.value = data.conn_fulcrum || '';
                if (connMonero) connMonero.value = data.conn_xmr_rpc || '';

                // Gotify
                var gotifyUrlEl = document.getElementById('notifGotifyUrl');
                var gotifyTokenEl = document.getElementById('notifGotifyToken');
                var enabledEl = document.getElementById('notifEnabled');
                var checkFreqEl = document.getElementById('checkFreq');
                var feeEnabledEl = document.getElementById('feeNotifEnabled');
                var feeThresholdEl = document.getElementById('feeThreshold');
                var feeAboveEl = document.getElementById('feeAboveThreshold');
                var newBlockEl = document.getElementById('newBlockNotif');
                var specificBlockEl = document.getElementById('specificBlockNotif');
                var specificBlockHeightEl = document.getElementById('specificBlockHeight');
                var txTargetConfsEl = document.getElementById('txTargetConfs');

                if (gotifyUrlEl) gotifyUrlEl.value = data.gotify_url || '';
                if (gotifyTokenEl) gotifyTokenEl.value = data.gotify_token || '';
                if (enabledEl) enabledEl.checked = data.notif_enabled || false;
                if (checkFreqEl) checkFreqEl.value = data.check_freq || 30;
                if (feeEnabledEl) feeEnabledEl.checked = data.fee_notif_enabled || false;
                if (feeThresholdEl) feeThresholdEl.value = data.fee_threshold || '';
                if (feeAboveEl) feeAboveEl.checked = data.fee_above_threshold || false;
                if (newBlockEl) newBlockEl.checked = data.new_block_notif || false;
                if (specificBlockEl) specificBlockEl.checked = data.specific_block_notif || false;
                if (specificBlockHeightEl) specificBlockHeightEl.value = data.specific_block_height || '';
                if (txTargetConfsEl) txTargetConfsEl.value = data.tx_target_confs || 1;

                // Show/hide content based on master toggle
                if (data.notif_enabled) {
                    notifContent.classList.add('visible');
                } else {
                    notifContent.classList.remove('visible');
                }

                // Update service bells
                var feeActive = data.fee_notif_enabled && !data.fee_above_threshold && data.fee_threshold > 0;
                var btcBell = document.getElementById('btcBell');
                var mpBell = document.getElementById('mpBell');
                var fulBell = document.getElementById('fulBell');
                var xmrBell = document.getElementById('xmrBell');
                if (btcBell) { if (feeActive) btcBell.classList.add('visible'); else btcBell.classList.remove('visible'); }
                if (mpBell) { if (feeActive) mpBell.classList.add('visible'); else mpBell.classList.remove('visible'); }
                if (fulBell) { if (data.new_block_notif || data.specific_block_notif) fulBell.classList.add('visible'); else fulBell.classList.remove('visible'); }
                if (xmrBell) { if (data.new_block_notif) xmrBell.classList.add('visible'); else xmrBell.classList.remove('visible'); }

                if (mpNotifIndicator) mpNotifIndicator.style.display = 'none';

                renderTxWatchList(data.tx_watches || []);
            })
            .catch(function(err) {
                console.error('Failed to load notification settings:', err);
            });
    }

    function renderTxWatchList(txWatches) {
        var list = document.getElementById('txWatchList');
        if (!list) return;
        list.innerHTML = '';
        txWatches.forEach(function(tx) {
            var item = document.createElement('div');
            item.className = 'tx-watch-item';
            var txPreview = tx.txid;
            if (txPreview.length > 16) {
                txPreview = txPreview.substring(0, 4) + '...' + txPreview.substring(txPreview.length - 4);
            }
            var status = tx.notified ? 'Notified' : (tx.confirmations > 0 ? tx.confirmations + ' confs' : 'Watching...');
            item.innerHTML = '<span class="txid" title="' + tx.txid + '">' + txPreview + '</span>' +
                '<span class="tx-status">' + status + '</span>' +
                '<button class="tx-remove" onclick="removeTxWatch(\'' + tx.txid + '\')">&times;</button>';
            list.appendChild(item);
        });
    }

    window.removeTxWatch = function(txid) {
        fetch('/api/notifications', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'remove_tx', txid: txid })
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.success) loadNotifSettings();
        })
        .catch(function() {});
    };

    window.clearAllTx = function() {
        fetch('/api/notifications', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'clear_tx' })
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.success) loadNotifSettings();
        })
        .catch(function() {});
    };

    window.addTxWatch = function() {
        var txid = document.getElementById('txIdInput').value.trim();
        if (txid.length !== 64 || !/^[0-9a-fA-F]+$/.test(txid)) {
            showNotifResult('save', 'Invalid TXID (must be 64 hex characters)', true);
            return;
        }
        var targetConfs = parseInt(document.getElementById('txTargetConfs').value) || 1;
        fetch('/api/notifications', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'save', txid: txid, tx_target_confs: targetConfs })
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.success) {
                document.getElementById('txIdInput').value = '';
                loadNotifSettings();
            } else {
                showNotifResult('save', data.message || 'Failed to add TX', true);
            }
        })
        .catch(function() {});
    };

    window.testGotify = function() {
        var gotifyUrl = document.getElementById('notifGotifyUrl').value.trim();
        var gotifyToken = document.getElementById('notifGotifyToken').value.trim();
        if (!gotifyUrl || !gotifyToken) {
            showNotifResult('test', 'Gotify URL and Token required', true);
            return;
        }
        // Validate URL format
        try {
            var urlObj = new URL(gotifyUrl);
            if (urlObj.protocol !== 'http:' && urlObj.protocol !== 'https:') {
                showNotifResult('test', 'Invalid URL (must be http or https)', true);
                return;
            }
        } catch(e) {
            showNotifResult('test', 'Invalid URL format', true);
            return;
        }
        showNotifResult('test', 'Testing...', false);
        fetch('/api/notifications', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ action: 'test', gotify_url: gotifyUrl, gotify_token: gotifyToken })
        })
        .then(function(r) {
            if (!r.ok) throw new Error('Network error');
            return r.json();
        })
        .then(function(data) {
            showNotifResult('test', data.message, !data.success);
        })
        .catch(function(err) {
            console.error('Gotify test error:', err);
            showNotifResult('test', 'Connection failed', true);
        });
    };

    window.saveNotifSettings = function() {
        var gotifyUrlEl = document.getElementById('notifGotifyUrl');
        var gotifyTokenEl = document.getElementById('notifGotifyToken');
        var enabledEl = document.getElementById('notifEnabled');
        var checkFreqEl = document.getElementById('checkFreq');
        var feeEnabledEl = document.getElementById('feeNotifEnabled');
        var feeThresholdEl = document.getElementById('feeThreshold');
        var feeAboveEl = document.getElementById('feeAboveThreshold');
        var newBlockEl = document.getElementById('newBlockNotif');
        var specificBlockEl = document.getElementById('specificBlockNotif');
        var specificBlockHeightEl = document.getElementById('specificBlockHeight');
        var txTargetConfsEl = document.getElementById('txTargetConfs');

        // Global settings
        var refreshIntervalEl = document.getElementById('refreshInterval');
        var showLatencyEl = document.getElementById('showLatency');
        var btcBlocksEl = document.getElementById('btcBlocksCount');
        var xmrBlocksEl = document.getElementById('xmrBlocksCount');

        if (!gotifyUrlEl || !enabledEl) {
            showNotifResult('save', 'UI error: elements not found', true);
            return;
        }

        var gotifyUrl = gotifyUrlEl.value.trim();
        var gotifyToken = gotifyTokenEl.value.trim();
        var enabled = enabledEl.checked;
        var checkFreq = parseInt(checkFreqEl.value) || 30;

        // Validate if notifications are enabled
        if (enabled) {
            if (!gotifyUrl) {
                showNotifResult('save', 'Gotify URL is required when notifications are enabled', true);
                return;
            }
            try {
                var urlObj = new URL(gotifyUrl);
                if (urlObj.protocol !== 'http:' && urlObj.protocol !== 'https:') {
                    showNotifResult('save', 'Invalid URL (must be http or https)', true);
                    return;
                }
            } catch(e) {
                showNotifResult('save', 'Invalid URL format', true);
                return;
            }
        }

        var svcMpEl = document.getElementById('svcMpEnabled');
        var svcFulEl = document.getElementById('svcFulEnabled');
        var svcXmrEl = document.getElementById('svcXmrEnabled');
        var connBtcRpc = document.getElementById('connBtcRpc');
        var connBtcUser = document.getElementById('connBtcUser');
        var connBtcPass = document.getElementById('connBtcPass');
        var connMpApi = document.getElementById('connMpApi');
        var connFulcrum = document.getElementById('connFulcrum');
        var connMonero = document.getElementById('connMonero');

        var body = {
            action: 'save',
            // Global settings
            refresh_interval: refreshIntervalEl ? parseInt(refreshIntervalEl.value) || 10 : 10,
            show_latency: showLatencyEl ? showLatencyEl.checked : true,
            btc_blocks_count: btcBlocksEl ? parseInt(btcBlocksEl.value) || 30 : 30,
            xmr_blocks_count: xmrBlocksEl ? parseInt(xmrBlocksEl.value) || 15 : 15,
            // Service connections
            svc_mp_enabled: svcMpEl ? svcMpEl.checked : true,
            svc_ful_enabled: svcFulEl ? svcFulEl.checked : true,
            svc_xmr_enabled: svcXmrEl ? svcXmrEl.checked : true,
            conn_btc_rpc: connBtcRpc ? connBtcRpc.value.trim() : '',
            conn_btc_user: connBtcUser ? connBtcUser.value.trim() : '',
            conn_btc_pass: connBtcPass ? connBtcPass.value.trim() : '',
            conn_mp_api: connMpApi ? connMpApi.value.trim() : '',
            conn_fulcrum: connFulcrum ? connFulcrum.value.trim() : '',
            conn_xmr_rpc: connMonero ? connMonero.value.trim() : '',
            // Notifications
            gotify_url: gotifyUrl,
            gotify_token: gotifyToken,
            notif_enabled: enabled,
            check_freq: checkFreq,
            fee_notif_enabled: feeEnabledEl ? feeEnabledEl.checked : false,
            fee_threshold: feeThresholdEl ? parseFloat(feeThresholdEl.value) || 0 : 0,
            fee_above_threshold: feeAboveEl ? feeAboveEl.checked : false,
            fee_type: 'next_block',
            new_block_notif: newBlockEl ? newBlockEl.checked : false,
            specific_block_notif: specificBlockEl ? specificBlockEl.checked : false,
            specific_block_height: specificBlockHeightEl ? parseInt(specificBlockHeightEl.value) || 0 : 0,
            tx_target_confs: txTargetConfsEl ? parseInt(txTargetConfsEl.value) || 1 : 1
        };
        showNotifResult('save', 'Saving...', false);
        fetch('/api/notifications', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        })
        .then(function(r) {
            if (!r.ok) throw new Error('Network error: ' + r.status);
            return r.json();
        })
        .then(function(data) {
            showNotifResult('save', data.message, !data.success);
            if (data.success) {
                // Reload the page after save to refresh everything
                setTimeout(function() { location.reload(); }, 1000);
            }
        })
        .catch(function(err) {
            console.error('[Settings] Save error:', err);
            showNotifResult('save', 'Connection failed: ' + err.message, true);
        });
    };

    function showNotifResult(type, message, isError) {
        var el = document.getElementById(type === 'test' ? 'notifTestResult' : 'notifSaveResult');
        if (!el) return;
        el.textContent = message;
        el.className = 'notif-result ' + (isError ? 'error' : 'success');
        if (!isError && type === 'save') {
            setTimeout(function() { el.className = 'notif-result'; }, 3000);
        }
    }

    function showSvcTestResult(resultId, message, isSuccess) {
        var el = document.getElementById(resultId);
        if (!el) return;
        el.textContent = message;
        el.className = 'svc-test-result ' + (isSuccess ? 'success' : 'error');
    }

    if ('serviceWorker' in navigator) {
        navigator.serviceWorker.register('/static/sw.js').catch(function() {});
    }

    // Collapsible sections
    function setupCollapsible(toggleId, contentId) {
        var toggle = document.getElementById(toggleId);
        var content = document.getElementById(contentId);
        if (toggle && content) {
            toggle.addEventListener('click', function(e) {
                e.preventDefault();
                e.stopPropagation();
                toggle.classList.toggle('open');
                content.classList.toggle('visible');
            });
        }
    }
    setupCollapsible('globalToggle', 'globalContent');
    setupCollapsible('connToggle', 'connContent');
    setupCollapsible('notifToggle', 'notifSection');

    // Password/token visibility toggles - use event delegation for dynamic content
    document.addEventListener('click', function(e) {
        if (e.target.classList.contains('toggle-password')) {
            e.preventDefault();
            e.stopPropagation();
            var targetId = e.target.getAttribute('data-target');
            var targetInput = document.getElementById(targetId);
            if (targetInput) {
                if (targetInput.type === 'password') {
                    targetInput.type = 'text';
                    e.target.textContent = '\uD83D\uDC41\uFE0F\u200D\uD83D\uDDE8\uFE0F';
                } else {
                    targetInput.type = 'password';
                    e.target.textContent = '\uD83D\uDC41';
                }
            }
        }
    });

    // Test connection buttons
    function testServiceConnection(name, url, resultId) {
        var resultEl = document.getElementById(resultId);
        if (!resultEl) return;
        resultEl.textContent = 'Testing ' + name + '...';
        resultEl.className = 'svc-test-result';
        var connBtcUser = document.getElementById('connBtcUser').value.trim();
        var connBtcPass = document.getElementById('connBtcPass').value.trim();
        fetch('/api/notifications', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                action: 'test_connection',
                test_name: name.toLowerCase().replace(' core', '').replace(' ', ''),
                test_url: url,
                conn_btc_user: connBtcUser,
                conn_btc_pass: connBtcPass
            })
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            resultEl.textContent = data.message || (data.success ? name + ' OK' : name + ' failed');
            resultEl.className = 'svc-test-result ' + (data.success ? 'success' : 'error');
            // Auto-hide after 5 seconds
            setTimeout(function() {
                resultEl.className = 'svc-test-result';
                resultEl.textContent = '';
            }, 5000);
        })
        .catch(function() {
            resultEl.textContent = name + ' test failed';
            resultEl.className = 'svc-test-result error';
            setTimeout(function() {
                resultEl.className = 'svc-test-result';
                resultEl.textContent = '';
            }, 5000);
        });
    }

    var testBtcBtn = document.getElementById('testBtcBtn');
    if (testBtcBtn) testBtcBtn.addEventListener('click', function() {
        var url = document.getElementById('connBtcRpc').value.trim();
        if (!url) { showSvcTestResult('testBtcResult', 'RPC Address required', false); return; }
        testServiceConnection('Bitcoin Core', url, 'testBtcResult');
    });
    var testMpBtn = document.getElementById('testMpBtn');
    if (testMpBtn) testMpBtn.addEventListener('click', function() {
        var url = document.getElementById('connMpApi').value.trim();
        if (!url) { showSvcTestResult('testMpResult', 'API Endpoint required', false); return; }
        // Auto-append /api if not present
        if (!url.endsWith('/api')) {
            url = url + '/api';
        }
        testServiceConnection('Mempool', url, 'testMpResult');
    });
    var testFulBtn = document.getElementById('testFulBtn');
    if (testFulBtn) testFulBtn.addEventListener('click', function() {
        var url = document.getElementById('connFulcrum').value.trim();
        if (!url) { showSvcTestResult('testFulResult', 'Address required', false); return; }
        testServiceConnection('Fulcrum', url, 'testFulResult');
    });
    var testXmrBtn = document.getElementById('testXmrBtn');
    if (testXmrBtn) testXmrBtn.addEventListener('click', function() {
        var url = document.getElementById('connMonero').value.trim();
        if (!url) { showSvcTestResult('testXmrResult', 'RPC Address required', false); return; }
        testServiceConnection('Monero', url, 'testXmrResult');
    });

    // Initialize all button event listeners (must be after all window.* functions are defined)
    function initButtonListeners() {
        var peerCloseBtn = document.getElementById('peerCloseBtn');
        if (peerCloseBtn) peerCloseBtn.addEventListener('click', closePeer);

        var xmrPeerCloseBtn = document.getElementById('xmrPeerCloseBtn');
        if (xmrPeerCloseBtn) xmrPeerCloseBtn.addEventListener('click', closeXmrPeer);

        var titleLinks = document.querySelectorAll('[data-title-link]');
        for (var i = 0; i < titleLinks.length; i++) {
            (function(el) {
                el.addEventListener('click', function() {
                    var url = el.getAttribute('data-title-link');
                    if (url) window.open(url, '_blank');
                });
            })(titleLinks[i]);
        }

        var testGotifyBtn = document.getElementById('testGotifyBtn');
        if (testGotifyBtn) testGotifyBtn.addEventListener('click', window.testGotify);

        var addTxWatchBtn = document.getElementById('addTxWatchBtn');
        if (addTxWatchBtn) addTxWatchBtn.addEventListener('click', window.addTxWatch);

        var clearAllTxBtn = document.getElementById('clearAllTxBtn');
        if (clearAllTxBtn) clearAllTxBtn.addEventListener('click', window.clearAllTx);

        var saveNotifBtn = document.getElementById('saveNotifBtn');
        if (saveNotifBtn) saveNotifBtn.addEventListener('click', window.saveNotifSettings);

        console.log('[Init] All button listeners attached');
    }

    initButtonListeners();

})();
