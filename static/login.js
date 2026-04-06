// NodeRouter - Login Page JavaScript
(function() {
  'use strict';

  // Tab switching
  const tabs = document.querySelectorAll('.login-tab');
  const panels = document.querySelectorAll('.login-panel');

  tabs.forEach(function(tab) {
    tab.addEventListener('click', function() {
      const target = tab.getAttribute('data-tab');
      
      // Update active tab
      tabs.forEach(function(t) { t.classList.remove('active'); });
      tab.classList.add('active');
      
      // Update active panel
      panels.forEach(function(p) { p.classList.remove('active'); });
      document.getElementById(target + 'Panel').classList.add('active');
    });
  });

  // Password visibility toggle - Apple style
  const showPasswordToggle = document.getElementById('showPassword');
  const passwordInput = document.getElementById('password');
  if (showPasswordToggle && passwordInput) {
    showPasswordToggle.addEventListener('change', function() {
      passwordInput.type = this.checked ? 'text' : 'password';
    });
  }

  // Password form submission
  const passwordForm = document.getElementById('passwordForm');
  if (passwordForm) {
    passwordForm.addEventListener('submit', function(e) {
      e.preventDefault();
      
      const password = passwordInput.value;
      const loginBtn = document.getElementById('loginBtn');
      const btnText = loginBtn.querySelector('.btn-text');
      const btnSpinner = loginBtn.querySelector('.btn-spinner');
      const message = document.getElementById('loginMessage');
      
      // Disable button and show spinner
      loginBtn.disabled = true;
      btnText.textContent = 'Logging in...';
      btnSpinner.classList.remove('hidden');
      message.className = 'login-message';
      message.textContent = '';
      
      fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: password })
      })
      .then(function(r) { return r.json(); })
      .then(function(data) {
        if (data.success) {
          message.className = 'login-message success';
          message.textContent = 'Login successful';
          // Reload to go to dashboard
          setTimeout(function() {
            window.location.href = '/';
          }, 800);
        } else {
          message.className = 'login-message error';
          message.textContent = data.message || 'Login failed';
          loginBtn.disabled = false;
          btnText.textContent = 'Login';
          btnSpinner.classList.add('hidden');
        }
      })
      .catch(function(err) {
        message.className = 'login-message error';
        message.textContent = 'Connection failed';
        loginBtn.disabled = false;
        btnText.textContent = 'Login';
        btnSpinner.classList.add('hidden');
      });
    });
  }

  // Auth47 QR code generation
  const qrContainer = document.getElementById('qrContainer');
  const qrLoading = document.getElementById('qrLoading');
  const qrImage = document.getElementById('qrImage');
  const auth47UriWrap = document.getElementById('auth47UriWrap');
  const auth47UriInput = document.getElementById('auth47Uri');
  const copyUriBtn = document.getElementById('copyUriBtn');
  const auth47Status = document.getElementById('auth47Status');
  let auth47PollInterval = null;
  let currentNonce = null;

  // Auto-generate QR on page load if Auth47 panel exists
  if (document.getElementById('auth47Panel')) {
    generateAuth47QR();
  }

  function generateAuth47QR() {
    if (qrLoading) qrLoading.style.display = 'flex';
    if (qrImage) {
      qrImage.style.display = 'none';
      // Remove any existing avatar overlay
      var existingAvatar = qrImage.parentElement.querySelector('.qr-avatar-overlay');
      if (existingAvatar) existingAvatar.remove();
    }
    if (auth47UriWrap) auth47UriWrap.style.display = 'none';
    if (auth47Status) {
      auth47Status.className = 'auth47-status';
      auth47Status.querySelector('.status-text').textContent = 'Waiting for scan...';
    }
    
    // Fetch Auth47 URI and payment code
    fetch('/auth/auth47/uri')
    .then(function(r) { return r.json(); })
    .then(function(data) {
      if (data.success) {
        currentNonce = data.nonce;
        var paymentCode = data.payment_code;
        
        // Generate QR code
        return fetch('/api/qr?text=' + encodeURIComponent(data.uri)).then(function(qrResp) {
          return qrResp.json().then(function(qrData) {
            return { qrData: qrData, paymentCode: paymentCode };
          });
        });
      } else {
        throw new Error(data.message || 'Failed to generate QR');
      }
    })
    .then(function(result) {
      var qrData = result.qrData;
      var paymentCode = result.paymentCode;
      
      if (qrData.qr) {
        if (qrImage) {
          qrImage.src = qrData.qr;
          qrImage.style.display = 'block';
        }
        if (qrLoading) qrLoading.style.display = 'none';
        
        // Fetch paynym avatar and display next to logo
        if (paymentCode) {
          fetchPaynymAvatar(paymentCode);
        }
        
        // Show URI with copy button
        if (auth47UriInput) {
          auth47UriInput.value = currentNonce ? 'auth47://' + currentNonce + '?...' : '';
          // Store full auth47 URI for copy (not the QR data URL!)
          auth47UriInput.dataset.fullUri = 'auth47://' + currentNonce + '?c=' + encodeURIComponent(window.location.origin + '/auth/auth47/callback') + '&e=' + Math.floor(Date.now() / 1000 + 600) + '&r=' + encodeURIComponent(window.location.origin + '/auth/auth47/callback');
        }
        if (auth47UriWrap) auth47UriWrap.style.display = 'flex';
        
        // Start polling
        if (currentNonce) startAuth47Polling(currentNonce);
      }
    })
    .catch(function(err) {
      if (qrLoading) {
        qrLoading.innerHTML = '<p style="color:var(--red)">Failed to generate QR. <button onclick="window.generateAuth47QR()" style="background:var(--blue);color:#fff;border:none;padding:4px 8px;border-radius:4px;cursor:pointer">Retry</button></p>';
      }
      console.error('Auth47 QR error:', err);
    });
  }

  // Fetch paynym avatar and display next to logo
  function fetchPaynymAvatar(paymentCode) {
    fetch('/api/paynym/avatar?code=' + encodeURIComponent(paymentCode))
    .then(function(r) { return r.json(); })
    .then(function(data) {
      if (data.avatar_url) {
        var avatar = document.getElementById('paynymAvatar');
        if (avatar) {
          avatar.src = data.avatar_url;
          avatar.style.display = 'block';
          // Trigger animation
          setTimeout(function() {
            avatar.classList.add('visible');
          }, 100);
        }
      }
    })
    .catch(function() {
      // Continue without avatar
    });
  }

  // Make retry function available globally
  window.generateAuth47QR = generateAuth47QR;

  // Copy URI button
  if (copyUriBtn && auth47UriInput) {
    copyUriBtn.addEventListener('click', function(e) {
      e.preventDefault();
      e.stopPropagation();
      
      // Build the full auth47 URI for copying
      var fullUri = auth47UriInput.dataset.fullUri;
      if (!fullUri && currentNonce) {
        // Reconstruct from the data we have
        fullUri = 'auth47://' + currentNonce + '?c=' + encodeURIComponent(window.location.origin + '/auth/auth47/callback') + '&e=' + Math.floor(Date.now() / 1000 + 600) + '&r=' + encodeURIComponent(window.location.origin + '/auth/auth47/callback');
      }
      if (!fullUri) fullUri = auth47UriInput.value;
      
      // Use fallback method first (more reliable)
      var textArea = document.createElement('textarea');
      textArea.value = fullUri;
      textArea.style.position = 'fixed';
      textArea.style.left = '-9999px';
      document.body.appendChild(textArea);
      textArea.select();
      try {
        document.execCommand('copy');
        copyUriBtn.classList.add('copied');
        copyUriBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 6L9 17l-5-5"/></svg>';
        setTimeout(function() {
          copyUriBtn.classList.remove('copied');
          copyUriBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>';
        }, 1500);
      } catch (err) {
        // Try modern clipboard API
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(fullUri).then(function() {
            copyUriBtn.classList.add('copied');
            copyUriBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 6L9 17l-5-5"/></svg>';
            setTimeout(function() {
              copyUriBtn.classList.remove('copied');
              copyUriBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>';
            }, 1500);
          });
        }
      }
      document.body.removeChild(textArea);
    });
  }

  function startAuth47Polling(nonce) {
    if (auth47PollInterval) {
      clearInterval(auth47PollInterval);
    }
    
    auth47PollInterval = setInterval(function() {
      fetch('/auth/auth47/status/' + nonce)
      .then(function(r) { return r.json(); })
      .then(function(data) {
        if (data.status === 'verified') {
          clearInterval(auth47PollInterval);
          if (auth47Status) {
            auth47Status.className = 'auth47-status verified';
            auth47Status.querySelector('.status-text').textContent = 'Authenticated! Redirecting...';
          }
          // Redirect to dashboard
          setTimeout(function() {
            window.location.href = '/';
          }, 1500);
        } else if (data.status === 'invalid') {
          clearInterval(auth47PollInterval);
          if (auth47Status) {
            auth47Status.className = 'auth47-status error';
            auth47Status.querySelector('.status-text').textContent = 'Authentication expired. Please retry.';
          }
          // Auto-regenerate
          setTimeout(function() {
            generateAuth47QR();
          }, 3000);
        }
      })
      .catch(function() {
        // Continue polling
      });
    }, 2000);
  }

})();
