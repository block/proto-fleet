// Package mqttlimits holds MQTT message bounds shared by transport and decoding.
package mqttlimits

// MaxPayloadBytes bounds MQTT bodies before copying and decoding.
const MaxPayloadBytes = 1024
