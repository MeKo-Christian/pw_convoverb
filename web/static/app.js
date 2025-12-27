(function() {
    'use strict';

    // DOM elements
    const statusEl = document.getElementById('status');
    const irSelect = document.getElementById('ir-select');
    const wetSlider = document.getElementById('wet-slider');
    const drySlider = document.getElementById('dry-slider');
    const wetValue = document.getElementById('wet-value');
    const dryValue = document.getElementById('dry-value');

    // Meter elements
    const meters = {
        inL: { bar: document.getElementById('meter-in-l'), val: document.getElementById('meter-in-l-val') },
        inR: { bar: document.getElementById('meter-in-r'), val: document.getElementById('meter-in-r-val') },
        revL: { bar: document.getElementById('meter-rev-l'), val: document.getElementById('meter-rev-l-val') },
        revR: { bar: document.getElementById('meter-rev-r'), val: document.getElementById('meter-rev-r-val') },
        outL: { bar: document.getElementById('meter-out-l'), val: document.getElementById('meter-out-l-val') },
        outR: { bar: document.getElementById('meter-out-r'), val: document.getElementById('meter-out-r-val') }
    };

    // State
    let ws = null;
    let reconnectTimer = null;
    let irList = [];
    let currentIRIndex = 0;
    let ignoreSliderChange = false;

    // Connect to WebSocket
    function connect() {
        const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(protocol + '//' + location.host + '/ws');

        ws.onopen = function() {
            statusEl.textContent = 'Connected';
            statusEl.className = 'status connected';
            clearTimeout(reconnectTimer);
        };

        ws.onclose = function() {
            statusEl.textContent = 'Disconnected';
            statusEl.className = 'status disconnected';
            ws = null;
            reconnectTimer = setTimeout(connect, 2000);
        };

        ws.onerror = function() {
            ws.close();
        };

        ws.onmessage = function(event) {
            try {
                const msg = JSON.parse(event.data);
                handleMessage(msg);
            } catch (e) {
                console.error('Failed to parse message:', e);
            }
        };
    }

    // Handle incoming messages
    function handleMessage(msg) {
        switch (msg.type) {
            case 'state':
                updateState(msg.payload);
                break;
            case 'ir_list':
                updateIRList(msg.payload);
                break;
            case 'meters':
                updateMeters(msg.payload);
                break;
            case 'param_changed':
                updateParam(msg.payload);
                break;
            case 'ir_changed':
                updateCurrentIR(msg.payload);
                break;
        }
    }

    // Update full state
    function updateState(state) {
        ignoreSliderChange = true;
        wetSlider.value = state.wet;
        drySlider.value = state.dry;
        wetValue.textContent = state.wet.toFixed(2);
        dryValue.textContent = state.dry.toFixed(2);
        currentIRIndex = state.irIndex;
        irSelect.value = state.irIndex;
        ignoreSliderChange = false;
    }

    // Update IR list
    function updateIRList(list) {
        irList = list;
        irSelect.innerHTML = '';

        list.forEach(function(ir) {
            const option = document.createElement('option');
            option.value = ir.index;
            option.textContent = ir.name + ' (' + ir.category + ', ' + (ir.sampleRate / 1000).toFixed(0) + 'kHz, ' + ir.duration.toFixed(1) + 's)';
            irSelect.appendChild(option);
        });

        irSelect.value = currentIRIndex;
    }

    // Update meters
    function updateMeters(m) {
        updateMeter(meters.inL, m.inL);
        updateMeter(meters.inR, m.inR);
        updateMeter(meters.revL, m.revL);
        updateMeter(meters.revR, m.revR);
        updateMeter(meters.outL, m.outL);
        updateMeter(meters.outR, m.outR);
    }

    // Update a single meter
    function updateMeter(meter, db) {
        // Convert dB to percentage (range: -96 to 6 dB)
        const minDB = -96;
        const maxDB = 6;
        const percent = Math.max(0, Math.min(100, ((db - minDB) / (maxDB - minDB)) * 100));
        meter.bar.style.width = percent + '%';
        meter.val.textContent = db.toFixed(1) + ' dB';
    }

    // Update single parameter
    function updateParam(payload) {
        ignoreSliderChange = true;
        if (payload.param === 'wet') {
            wetSlider.value = payload.value;
            wetValue.textContent = payload.value.toFixed(2);
        } else if (payload.param === 'dry') {
            drySlider.value = payload.value;
            dryValue.textContent = payload.value.toFixed(2);
        }
        ignoreSliderChange = false;
    }

    // Update current IR
    function updateCurrentIR(payload) {
        currentIRIndex = payload.index;
        irSelect.value = payload.index;
    }

    // Send message to server
    function send(type, payload) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: type, payload: payload }));
        }
    }

    // Event handlers
    wetSlider.addEventListener('input', function() {
        const value = parseFloat(this.value);
        wetValue.textContent = value.toFixed(2);
        if (!ignoreSliderChange) {
            send('set_wet', { value: value });
        }
    });

    drySlider.addEventListener('input', function() {
        const value = parseFloat(this.value);
        dryValue.textContent = value.toFixed(2);
        if (!ignoreSliderChange) {
            send('set_dry', { value: value });
        }
    });

    irSelect.addEventListener('change', function() {
        const index = parseInt(this.value, 10);
        send('set_ir', { index: index });
    });

    // Start connection
    connect();
})();
