export default {
	data() {
		return {
			ws: null,
			reconnectAttempts: 0,
			maxReconnectAttempts: 5,
			reconnectDelay: 1000, // Start with 1 second delay
			channels: {
				subscribed: new Set(),
				listeners: {},
				whispers: {}, // New container for whisper listeners
			},
			connected: false,
		};
	},

	created() {
		// Initialize WebSocket connection when the component is created
		this.connect();
	},

	beforeDestroy() {
		// Clean up WebSocket connection when the component is destroyed
		this.disconnect();
	},

	methods: {
		/**
		 * Establish WebSocket connection
		 */
		connect() {
			// Get the WebSocket URL from Nuxt config, removing the scheme if present
			let wsUrl = this.$config.chatServiceUrl || "";

			// Remove scheme if present (http:// or https://)
			wsUrl = wsUrl.replace(/^(http|https):\/\//, "");

			// Get authentication token from store
			const token = this.$store.getters["auth/token"];

			// Use secure WebSocket protocol with token if available
			let fullWsUrl = `wss://${wsUrl}/ws`;

			// Append token as query parameter if available
			if (token) {
				fullWsUrl += `?token=${token}`;
			}

			console.log(`Attempting to connect to WebSocket server at ${fullWsUrl}`);
			this.ws = new WebSocket(fullWsUrl);

			this.ws.onopen = this.handleOpen;
			this.ws.onmessage = this.handleMessage;
			this.ws.onclose = this.handleClose;
			this.ws.onerror = this.handleError;
		},

		/**
		 * Disconnect WebSocket connection
		 */
		disconnect() {
			if (this.ws && this.ws.readyState === WebSocket.OPEN) {
				this.ws.close();
			}
		},

		/**
		 * Handle WebSocket open event
		 */
		handleOpen() {
			console.log("%cWebSocket connection established successfully", "color: green; font-weight: bold");
			this.connected = true;
			this.reconnectAttempts = 0; // Reset reconnect attempts on successful connection

			// Resubscribe to all previously subscribed channels
			this.channels.subscribed.forEach((channel) => {
				this.sendSubscription(channel);
			});

			// Emit connected event
			this.$emit("echo:connected");
		},

		/**
		 * Handle WebSocket message event
		 * @param {MessageEvent} evt - The message event
		 */
		handleMessage(evt) {
			console.log("%cMessage received:", "color: purple", {
				timestamp: new Date().toISOString(),
				data: evt.data,
        event: evt,
			});

			try {
				const data = JSON.parse(evt.data);
				const channel = data.channel_name;
				const action = data.action;
				const event = data.event;

        console.log('Message data:', { channel, action, event, data });

				if (channel && event) {
					if (action === "whisper") {
						this.triggerWhisperEvent(channel, event, data.data);
					} else {
						this.triggerEvent(channel, event, data.data);
					}
				}

				// Emit a global message event
				this.$emit("echo:message", data);
			} catch (error) {
				console.error("Error parsing WebSocket message:", error);
			}
		},

		/**
		 * Handle WebSocket close event
		 * @param {CloseEvent} event - The close event
		 */
		handleClose(event) {
			console.log("%cWebSocket connection closed", "color: orange", {
				code: event.code,
				reason: event.reason,
				wasClean: event.wasClean,
				timestamp: new Date().toISOString(),
			});

			this.connected = false;

			// Emit disconnected event
			this.$emit("echo:disconnected", event);

			// Attempt to reconnect
			if (this.reconnectAttempts < this.maxReconnectAttempts) {
				const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts); // Exponential backoff
				console.log(`Attempting to reconnect in ${delay / 1000} seconds... (Attempt ${this.reconnectAttempts + 1}/${this.maxReconnectAttempts})`);

				setTimeout(() => {
					this.reconnectAttempts++;
					this.connect();
				}, delay);
			} else {
				console.error("%cMax reconnection attempts reached. Please refresh the page.", "color: red; font-weight: bold");
				// Emit reconnect failed event
				this.$emit("echo:reconnect-failed");
			}
		},

		/**
		 * Handle WebSocket error event
		 * @param {Event} err - The error event
		 */
		handleError(err) {
			console.error("%cWebSocket error occurred:", "color: red", {
				error: err,
				readyState: this.ws ? this.ws.readyState : "unknown",
				timestamp: new Date().toISOString(),
				connectionDetails: this.ws
					? {
							url: this.ws.url,
							protocol: this.ws.protocol,
							bufferedAmount: this.ws.bufferedAmount,
					  }
					: "unknown",
			});

			// Emit error event
			this.$emit("echo:error", err);
		},

		/**
		 * Send a subscription request for a channel
		 * @param {string} channel - The channel to subscribe to
		 */
		sendSubscription(channel) {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
				console.error("Cannot subscribe - WebSocket is not connected");
				return false;
			}

			const subscription = {
				action: "subscribe",
				channel: channel,
			};

			console.log("%cSubscribing to channel:", "color: blue", subscription);
			this.ws.send(JSON.stringify(subscription));
			return true;
		},

		/**
		 * Subscribe to a channel
		 * @param {string} channel - The channel to subscribe to
		 * @returns {object} - Channel object with listen, listenForWhisper, and whisper methods
		 */
		subscribe(channel) {
			// Add to subscribed channels set
			this.channels.subscribed.add(channel);

			// Initialize listeners for this channel if not already done
			if (!this.channels.listeners[channel]) {
				this.channels.listeners[channel] = {};
			}
			// Initialize whisper listeners for this channel if not already done
			if (!this.channels.whispers[channel]) {
				this.channels.whispers[channel] = {};
			}

			// Send subscription if connected
			if (this.connected) {
				this.sendSubscription(channel);
			}

			// Return channel object with listen, listenForWhisper, and whisper methods
			return {
				listen: (event, callback) => this.listen(channel, event, callback),
				listenForWhisper: (event, callback) => this.listenForWhisper(channel, event, callback),
				whisper: (event, data) => this.whisper(channel, event, data),
			};
		},

		/**
		 * Subscribe to a channel (alias for subscribe)
		 * @param {string} channel - The channel to subscribe to
		 * @returns {object} - Channel object with listen, listenForWhisper, and whisper methods
		 */
		channel(channel) {
			return this.subscribe(channel);
		},

		/**
		 * Subscribe to a private channel
		 * @param {string} channel - The private channel name without 'private-' prefix
		 * @returns {object} - Channel object with listen, listenForWhisper, and whisper methods
		 */
		private(channel) {
			return this.subscribe(`private-${channel}`);
		},

		/**
		 * Subscribe to a presence channel
		 * @param {string} channel - The presence channel name without 'presence-' prefix
		 * @returns {object} - Channel object with listen, listenForWhisper, and whisper methods
		 */
		presence(channel) {
			return this.subscribe(`presence-${channel}`);
		},

		/**
		 * Listen for an event on a channel
		 * @param {string} channel - The channel to listen on
		 * @param {string} event - The event to listen for
		 * @param {function} callback - The callback to execute when the event is received
		 * @returns {object} - The channel object for chaining
		 */
		listen(channel, event, callback) {
			if (!this.channels.listeners[channel]) {
				this.channels.listeners[channel] = {};
			}

			if (!this.channels.listeners[channel][event]) {
				this.channels.listeners[channel][event] = [];
			}

			this.channels.listeners[channel][event].push(callback);

			return {
				listen: (nextEvent, nextCallback) => this.listen(channel, nextEvent, nextCallback),
			};
		},

		/**
		 * Listen for a whisper event on a channel
		 * @param {string} channel - The channel to listen on
		 * @param {string} event - The whisper event to listen for
		 * @param {function} callback - The callback to execute when the whisper event is received
		 * @returns {object} - The channel object for chaining
		 */
		listenForWhisper(channel, event, callback) {
			if (!this.channels.whispers[channel]) {
				this.channels.whispers[channel] = {};
			}

			if (!this.channels.whispers[channel][event]) {
				this.channels.whispers[channel][event] = [];
			}

			this.channels.whispers[channel][event].push(callback);

			return {
				listenForWhisper: (nextEvent, nextCallback) => this.listenForWhisper(channel, nextEvent, nextCallback),
			};
		},

		/**
		 * Trigger an event for all regular listeners
		 * @param {string} channel - The channel the event was received on
		 * @param {string} event - The event name
		 * @param {any} data - The event data
		 */
		triggerEvent(channel, event, data) {
			if (this.channels.listeners[channel] && this.channels.listeners[channel][event]) {
				this.channels.listeners[channel][event].forEach((callback) => {
					callback(data);
				});
			}
		},

		/**
		 * Trigger an event for all whisper listeners
		 * @param {string} channel - The channel the whisper was received on
		 * @param {string} event - The whisper event name
		 * @param {any} data - The whisper event data
		 */
		triggerWhisperEvent(channel, event, data) {
			if (this.channels.whispers[channel] && this.channels.whispers[channel][event]) {
				this.channels.whispers[channel][event].forEach((callback) => {
					callback(data);
				});
			}
		},

		/**
		 * Send a whisper message to a channel
		 * @param {string} channel - The channel to send to
		 * @param {string} event - The whisper event name
		 * @param {any} data - The data to send
		 * @returns {boolean} - Whether the message was sent
		 */
		whisper(channel, event, data) {
			if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
				console.error("%cCannot send message - WebSocket is not connected", "color: red", {
					readyState: this.ws ? this.ws.readyState : "unknown",
					readyStateExplanation: this.ws
						? {
								0: "CONNECTING",
								1: "OPEN",
								2: "CLOSING",
								3: "CLOSED",
						  }[this.ws.readyState]
						: "unknown",
					timestamp: new Date().toISOString(),
				});
				return false;
			}

			// Send message with action "whisper"
			const message = {
				action: "whisper",
				channel: channel,
				event: event,
				data: data,
			};

			this.ws.send(JSON.stringify(message));
			return true;
		},

		/**
		 * Leave a channel
		 * @param {string} channel - The channel to leave
		 */
		leave(channel) {
			if (this.channels.subscribed.has(channel)) {
				// Remove from subscribed channels
				this.channels.subscribed.delete(channel);

				// Remove all listeners and whisper listeners
				delete this.channels.listeners[channel];
				delete this.channels.whispers[channel];

				// Send unsubscribe message if connected
				if (this.connected) {
					const unsubscribe = {
						action: "unsubscribe",
						channel: channel,
					};

					console.log("%cUnsubscribing from channel:", "color: blue", unsubscribe);
					this.ws.send(JSON.stringify(unsubscribe));
				}
			}
		},
	},
};
