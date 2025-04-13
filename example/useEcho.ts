import { useState, useEffect, useRef, useCallback } from 'react';

interface EchoOptions {
  url?: string;
  path?: string;
  token?: string;
  maxReconnectAttempts?: number;
  reconnectDelay?: number;
}

interface ChannelObject {
  listen: (event: string, callback: (data: any) => void) => ChannelObject;
  listenForWhisper: (event: string, callback: (data: any) => void) => ChannelObject;
  whisper: (event: string, data: any) => boolean;
}

export const useEcho = (options: EchoOptions) => {
  const [connected, setConnected] = useState<boolean>(false);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectAttemptsRef = useRef<number>(0);
  const maxReconnectAttempts = options.maxReconnectAttempts || 5;
  const reconnectDelay = options.reconnectDelay || 1000;
  
  const channelsRef = useRef<{
    subscribed: Set<string>;
    listeners: Record<string, Record<string, Array<(data: any) => void>>>;
    whispers: Record<string, Record<string, Array<(data: any) => void>>>;
  }>({
    subscribed: new Set(),
    listeners: {},
    whispers: {},
  });

  const connect = useCallback(() => {
    // Don't reconnect if already connected
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      return;
    }

    let wsUrl = "";
    
    // Use current URL if options.url is not provided or empty
    if (!options.url) {
      const currentUrl = window.location.host;
      const isSecure = window.location.protocol === 'https:';
      const wsProtocol = isSecure ? 'wss://' : 'ws://';
      wsUrl = `${wsProtocol}${currentUrl}`;
    } else {
      // Remove scheme if present (http:// or https://)
      wsUrl = options.url.replace(/^(http|https):\/\//, "");
      
      // Determine protocol based on current page protocol
      const isSecure = window.location.protocol === 'https:';
      const wsProtocol = isSecure ? 'wss://' : 'ws://';
      wsUrl = `${wsProtocol}${wsUrl}`;
    }

    if (options.path) {
      wsUrl += options.path;
    } else {
      wsUrl += "/ws";
    }

    // Append token as query parameter if available
    if (options.token) {
      wsUrl += `?token=${options.token}`;
    }

    console.log(`Attempting to connect to WebSocket server at ${wsUrl}`);
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = handleOpen;
    ws.onmessage = handleMessage;
    ws.onclose = handleClose;
    ws.onerror = handleError;
  }, [options.url, options.token]);

  const disconnect = useCallback(() => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.close();
    }
  }, []);

  const handleOpen = useCallback(() => {
    console.log("%cWebSocket connection established successfully", "color: green; font-weight: bold");
    setConnected(true);
    reconnectAttemptsRef.current = 0;

    // Resubscribe to all previously subscribed channels
    channelsRef.current.subscribed.forEach((channel) => {
      sendSubscription(channel);
    });
  }, []);

  const sendSubscription = useCallback((channel: string): boolean => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      console.error("Cannot subscribe - WebSocket is not connected");
      return false;
    }

    const subscription = {
      action: "subscribe",
      channel: channel,
    };

    console.log("%cSubscribing to channel:", "color: blue", subscription);
    wsRef.current.send(JSON.stringify(subscription));
    return true;
  }, []);

  const handleMessage = useCallback((evt: MessageEvent) => {
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
          triggerWhisperEvent(channel, event, data.data);
        } else {
          triggerEvent(channel, event, data.data);
        }
      }
    } catch (error) {
      console.error("Error parsing WebSocket message:", error);
    }
  }, []);

  const handleClose = useCallback((event: CloseEvent) => {
    console.log("%cWebSocket connection closed", "color: orange", {
      code: event.code,
      reason: event.reason,
      wasClean: event.wasClean,
      timestamp: new Date().toISOString(),
    });

    setConnected(false);

    // Attempt to reconnect
    if (reconnectAttemptsRef.current < maxReconnectAttempts) {
      const delay = reconnectDelay * Math.pow(2, reconnectAttemptsRef.current); // Exponential backoff
      console.log(`Attempting to reconnect in ${delay / 1000} seconds... (Attempt ${reconnectAttemptsRef.current + 1}/${maxReconnectAttempts})`);

      setTimeout(() => {
        reconnectAttemptsRef.current++;
        connect();
      }, delay);
    } else {
      console.error("%cMax reconnection attempts reached. Please refresh the page.", "color: red; font-weight: bold");
    }
  }, [connect, maxReconnectAttempts, reconnectDelay]);

  const handleError = useCallback((err: Event) => {
    console.error("%cWebSocket error occurred:", "color: red", {
      error: err,
      readyState: wsRef.current ? wsRef.current.readyState : "unknown",
      timestamp: new Date().toISOString(),
      connectionDetails: wsRef.current
        ? {
            url: wsRef.current.url,
            protocol: wsRef.current.protocol,
            bufferedAmount: wsRef.current.bufferedAmount,
          }
        : "unknown",
    });
  }, []);

  const triggerEvent = useCallback((channel: string, event: string, data: any) => {
    if (
      channelsRef.current.listeners[channel] &&
      channelsRef.current.listeners[channel][event]
    ) {
      channelsRef.current.listeners[channel][event].forEach((callback) => {
        callback(data);
      });
    }
  }, []);

  const triggerWhisperEvent = useCallback((channel: string, event: string, data: any) => {
    if (
      channelsRef.current.whispers[channel] &&
      channelsRef.current.whispers[channel][event]
    ) {
      channelsRef.current.whispers[channel][event].forEach((callback) => {
        callback(data);
      });
    }
  }, []);

  const subscribe = useCallback((channel: string): ChannelObject => {
    // Check if already subscribed
    if (!channelsRef.current.subscribed.has(channel)) {
      // Add to subscribed channels set
      channelsRef.current.subscribed.add(channel);

      // Initialize listeners for this channel if not already done
      if (!channelsRef.current.listeners[channel]) {
        channelsRef.current.listeners[channel] = {};
      }
      
      // Initialize whisper listeners for this channel if not already done
      if (!channelsRef.current.whispers[channel]) {
        channelsRef.current.whispers[channel] = {};
      }

      // Send subscription if connected
      if (connected) {
        sendSubscription(channel);
      }
    }

    // Return channel object with listen, listenForWhisper, and whisper methods
    return {
      listen: (event: string, callback: (data: any) => void): ChannelObject => 
        listen(channel, event, callback),
      listenForWhisper: (event: string, callback: (data: any) => void): ChannelObject => 
        listenForWhisper(channel, event, callback),
      whisper: (event: string, data: any): boolean => 
        whisper(channel, event, data),
    };
  }, [connected, sendSubscription]);

  const listen = useCallback((channel: string, event: string, callback: (data: any) => void): ChannelObject => {
    if (!channelsRef.current.listeners[channel]) {
      channelsRef.current.listeners[channel] = {};
    }

    if (!channelsRef.current.listeners[channel][event]) {
      channelsRef.current.listeners[channel][event] = [];
    }

    // Check if the callback is already registered to avoid duplicates
    if (!channelsRef.current.listeners[channel][event].includes(callback)) {
      channelsRef.current.listeners[channel][event].push(callback);
    }

    return {
      listen: (nextEvent: string, nextCallback: (data: any) => void): ChannelObject => 
        listen(channel, nextEvent, nextCallback),
      listenForWhisper: (event: string, callback: (data: any) => void): ChannelObject => 
        listenForWhisper(channel, event, callback),
      whisper: (event: string, data: any): boolean => 
        whisper(channel, event, data),
    };
  }, []);

  const listenForWhisper = useCallback((channel: string, event: string, callback: (data: any) => void): ChannelObject => {
    if (!channelsRef.current.whispers[channel]) {
      channelsRef.current.whispers[channel] = {};
    }

    if (!channelsRef.current.whispers[channel][event]) {
      channelsRef.current.whispers[channel][event] = [];
    }

    // Check if the callback is already registered to avoid duplicates
    if (!channelsRef.current.whispers[channel][event].includes(callback)) {
      channelsRef.current.whispers[channel][event].push(callback);
    }

    return {
      listen: (nextEvent: string, nextCallback: (data: any) => void): ChannelObject => 
        listen(channel, nextEvent, nextCallback),
      listenForWhisper: (nextEvent: string, nextCallback: (data: any) => void): ChannelObject => 
        listenForWhisper(channel, nextEvent, nextCallback),
      whisper: (event: string, data: any): boolean => 
        whisper(channel, event, data),
    };
  }, []);

  const whisper = useCallback((channel: string, event: string, data: any): boolean => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      console.error("%cCannot send message - WebSocket is not connected", "color: red", {
        readyState: wsRef.current ? wsRef.current.readyState : "unknown",
        readyStateExplanation: wsRef.current
          ? {
              0: "CONNECTING",
              1: "OPEN",
              2: "CLOSING",
              3: "CLOSED",
            }[wsRef.current.readyState]
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

    wsRef.current.send(JSON.stringify(message));
    return true;
  }, []);

  const leave = useCallback((channel: string) => {
    if (channelsRef.current.subscribed.has(channel)) {
      // Remove from subscribed channels
      channelsRef.current.subscribed.delete(channel);

      // Remove all listeners and whisper listeners
      delete channelsRef.current.listeners[channel];
      delete channelsRef.current.whispers[channel];

      // Send unsubscribe message if connected
      if (connected && wsRef.current) {
        const unsubscribe = {
          action: "unsubscribe",
          channel: channel,
        };

        console.log("%cUnsubscribing from channel:", "color: blue", unsubscribe);
        wsRef.current.send(JSON.stringify(unsubscribe));
      }
    }
  }, [connected]);

  // Connection lifecycle with useEffect
  useEffect(() => {
    connect();
    
    return () => {
      disconnect();
    };
  }, [connect, disconnect]);

  // Return the Echo API
  return {
    connected,
    subscribe,
    channel: subscribe, // Alias for subscribe
    private: (channel: string): ChannelObject => subscribe(`private-${channel}`),
    presence: (channel: string): ChannelObject => subscribe(`presence-${channel}`),
    leave,
    disconnect,
    connect,
  };
};

export default useEcho;
