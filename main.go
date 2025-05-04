package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tete1030/go-modemmanager"
)

const namespace = "modem"
const subsystem = ""

var (
	modemlabels = []string{"imei", "icc", "imsi", "operatorid", "operator", "v_operator", "rat"}

	listenAddress = flag.String("web.listen-address", ":9898",
		"Address to listen on for telemetry")
	metricsPath = flag.String("web.telemetry-path", "/metrics",
		"Path under which to expose metrics")

	up = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "up"),
		"Was the last modem query successful",
		nil, nil,
	)

	roaming = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "roaming"),
		"Is the modem roaming",
		modemlabels, nil,
	)

	operatorcode = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "operatorcode"),
		"Code of the operator currently used by the modem",
		modemlabels, nil,
	)

	rssi = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "rssi"),
		"Level of signal reported by the modem",
		modemlabels, nil,
	)

	rsrp = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "rsrp"),
		"Level of noise reported by the modem",
		modemlabels, nil,
	)

	registered = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "registered"),
		"Is the modem registered",
		modemlabels, nil,
	)

	connected = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "connected"),
		"Is the modem connected",
		modemlabels, nil,
	)

	cellid = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "cellid"),
		"CellID currently used by the modem",
		modemlabels, nil,
	)

	tac = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "tac"),
		"TAC currently used by the modem",
		modemlabels, nil,
	)

	lac = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "lac"),
		"LAC currently used by the modem",
		modemlabels, nil,
	)
)

type Exporter struct {
	mmgr modemmanager.ModemManager
}

func NewExporter(mmgr modemmanager.ModemManager) *Exporter {
	return &Exporter{
		mmgr: mmgr,
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- up
	ch <- operatorcode
	ch <- rssi
	ch <- rsrp
	ch <- roaming
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {

	modems, err := e.mmgr.GetModems()
	if err != nil {
		log.Println(err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		up, prometheus.GaugeValue, 1,
	)

	for _, modem := range modems {

		state, err := modem.GetState()
		if err != nil {
			log.Println("cannot get modem state:" + err.Error())
			continue
		}

		if state.String() == "Disabled" {
			log.Println("modem disabled, trying to enable it")
			err = modem.Enable()
			if err != nil {
				log.Println(err)
				continue
			}
		}

		sim, err := modem.GetSim()
		if err != nil {
			log.Println(err)
			continue
		}

		simIdent, err := sim.GetSimIdentifier()
		if err != nil {
			log.Println(err)
			continue
		}

		simImsi, err := sim.GetImsi()
		if err != nil {
			log.Println(err)
			continue
		}

		simOpIdent, err := sim.GetOperatorIdentifier()
		if err != nil {
			log.Println(err)
			continue
		}

		simOp, err := sim.GetOperatorName()
		if err != nil {
			log.Println(err)
			continue
		}

		modem3gpp, err := modem.Get3gpp()
		if err != nil {
			log.Println(err)
			continue
		}
		imei, err := modem3gpp.GetImei()
		if err != nil {
			log.Println(err)
			continue
		}

		opName, err := modem3gpp.GetOperatorName()
		if err != nil {
			log.Println(err)
			continue
		}

		ratList, err := modem.GetAccessTechnologies()
		if err != nil {
			log.Println(err)
			continue
		}

		if len(ratList) != 1 {
			log.Println(err)
			continue
		}

		rat := strings.ToLower(ratList[0].String())

		state, err = modem.GetState()
		if err != nil {
			log.Println("cannot get modem state:" + err.Error())
			continue
		}

		// if we are registered, we should try to connect
		if state.String() == "Registered" {

			apn := os.Getenv("MODEM_EXPORTER_APN")

			if apn != "" {

				bearers, _ := modem.GetBearers()

				// delete all bearer - if registered but no bearer something is likely wrong
				for _, bearer := range bearers {
					bearer.Disconnect()
					err = modem.DeleteBearer(bearer)
					if err != nil {
						log.Println(err)
						continue
					}
				}

				modemSimple, err := modem.GetSimpleModem()
				if err != nil {
					log.Println(err)
				} else {
					property := modemmanager.SimpleProperties{Apn: apn}
					newBearer, err := modemSimple.Connect(property)
					if err != nil {
						log.Println(err)
					} else {
						fmt.Println("New Bearer: ", newBearer)
					}
				}

			}

		}

		if state.String() == "Registered" || state.String() == "Connected" {
			ch <- prometheus.MustNewConstMetric(
				registered, prometheus.GaugeValue, 1, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		} else {
			ch <- prometheus.MustNewConstMetric(
				registered, prometheus.GaugeValue, 0, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		}

		if state.String() == "Connected" {
			ch <- prometheus.MustNewConstMetric(
				connected, prometheus.GaugeValue, 1, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		} else {
			ch <- prometheus.MustNewConstMetric(
				connected, prometheus.GaugeValue, 0, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		}

		// failure reason
		_, err = modem.GetStateFailedReason()
		if err != nil {
			log.Println(err)
			continue
		}

		modemLocation, err := modem.GetLocation()
		if err != nil {
			log.Println(err)
			continue
		}

		mloc, err := modemLocation.GetCurrentLocation()
		if err != nil {
			log.Println(err)
			continue
		}

		cellID := mloc.ThreeGppLacCi.Ci

		if decCellID, err := strconv.ParseInt(cellID, 16, 64); err == nil {
			ch <- prometheus.MustNewConstMetric(
				cellid, prometheus.GaugeValue, float64(decCellID), imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		} else {
			log.Println(err)
		}

		lAC := mloc.ThreeGppLacCi.Lac
		if decLAC, err := strconv.ParseInt(lAC, 16, 64); err == nil {
			ch <- prometheus.MustNewConstMetric(
				lac, prometheus.GaugeValue, float64(decLAC), imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		} else {
			log.Println(err)
		}

		tAC := mloc.ThreeGppLacCi.Tac
		if decTAC, err := strconv.ParseInt(tAC, 16, 64); err == nil {
			ch <- prometheus.MustNewConstMetric(
				tac, prometheus.GaugeValue, float64(decTAC), imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		} else {
			log.Println(err)
		}

		regState, err := modem3gpp.GetRegistrationState()
		if err != nil {
			log.Println(err)
			continue
		}

		if regState.String() == "Roaming" {
			ch <- prometheus.MustNewConstMetric(
				roaming, prometheus.GaugeValue, 1, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		} else {
			ch <- prometheus.MustNewConstMetric(
				roaming, prometheus.GaugeValue, 0, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		}

		opCode, err := modem3gpp.GetOperatorCode()
		if err != nil {
			log.Println(err)
			continue
		}

		if s, err := strconv.ParseFloat(opCode, 64); err == nil {
			ch <- prometheus.MustNewConstMetric(
				operatorcode, prometheus.GaugeValue, s, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		}

		modemSignal, err := modem.GetSignal()
		if err != nil {
			log.Println(err)
			continue
		}

		err = modemSignal.Setup(1)
		if err != nil {
			log.Println(err)
			continue
		}

		time.Sleep(2 * time.Second)

		currentSignal, err := modemSignal.GetCurrentSignals()
		if err != nil {
			log.Println(err)
			continue
		}

		for _, sp := range currentSignal {
			ch <- prometheus.MustNewConstMetric(
				rssi, prometheus.GaugeValue, sp.Rssi, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)

			ch <- prometheus.MustNewConstMetric(
				rsrp, prometheus.GaugeValue, sp.Rsrp, imei, simIdent, simImsi, simOpIdent, simOp, opName, rat,
			)
		}

		err = modemSignal.Setup(0)
		if err != nil {
			log.Println(err)
			continue
		}

	}

}

func influxHandler(mmgr modemmanager.ModemManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modems, err := mmgr.GetModems()
		if err != nil {
			log.Println("error getting modems:", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error getting modems: " + err.Error()))
			return
		}
		for _, modem := range modems {
			modem3gpp, err := modem.Get3gpp()
			if err != nil {
				log.Println("error getting 3gpp:", err)
				continue
			}
			imei, _ := modem3gpp.GetImei()
			sim, err := modem.GetSim()
			if err != nil {
				log.Println("error getting sim:", err)
				continue
			}
			simIdent, err := sim.GetSimIdentifier()
			if err != nil {
				log.Println("error getting sim identifier:", err)
				continue
			}
			simImsi, err := sim.GetImsi()
			if err != nil {
				log.Println("error getting sim imsi:", err)
				continue
			}

			simOpIdent, err := sim.GetOperatorIdentifier()
			if err != nil {
				log.Println("error getting sim operator identifier:", err)
			}
			simOp, err := sim.GetOperatorName()
			if err != nil {
				log.Println("error getting sim operator name:", err)
			}

			opName, err := modem3gpp.GetOperatorName()
			if err != nil {
				log.Println("error getting operator name:", err)
			}

			mcc, err := modem3gpp.GetMcc()
			if err != nil {
				log.Println("error getting mcc:", err)
			}
			mnc, err := modem3gpp.GetMnc()
			if err != nil {
				log.Println("error getting mnc:", err)
			}

			ratList, err := modem.GetAccessTechnologies()
			if err != nil {
				log.Println("error getting access technologies:", err)
				continue
			}
			rat := ""
			if len(ratList) > 0 {
				rat = strings.ToLower(ratList[0].String())
				if len(ratList) > 1 {
					log.Println("multiple access technologies found, using first one. Dropped:", ratList[1:])
				}
			}

			phoneNumber := ""
			phoneNumberList, err := modem.GetOwnNumbers()
			if err != nil {
				log.Println("error getting own numbers:", err)
			} else {
				if len(phoneNumberList) > 0 {
					phoneNumber = phoneNumberList[0]
					if len(phoneNumberList) > 1 {
						log.Println("multiple own numbers found, using first one. Dropped:", phoneNumberList[1:])
					}
				}
			}

			tags := fmt.Sprintf(
				"imei=%s,icc=%s,imsi=%s,operatorid=%s,operator=%s,v_operator=%s,mcc=%s,mnc=%s,rat=%s,phone_number=%s",
				imei, simIdent, simImsi, simOpIdent, simOp, opName, mcc, mnc, rat, phoneNumber,
			)
			timestamp := time.Now().UnixNano()

			powerState, err := modem.GetPowerState()
			if err != nil {
				log.Println("error getting power state:", err)
			}

			signalQuality, signalQualityRecent, err := modem.GetSignalQuality()
			if err != nil {
				log.Println("error getting signal quality:", err)
			}

			cellID := ""
			lac := ""
			tac := ""
			modemLocation, err := modem.GetLocation()
			if err != nil {
				log.Println("error getting modem location:", err)
			} else {
				mloc, err := modemLocation.GetCurrentLocation()
				if err != nil {
					log.Println("error getting current location:", err)
				} else {
					cellID = mloc.ThreeGppLacCi.Ci
					lac = mloc.ThreeGppLacCi.Lac
					tac = mloc.ThreeGppLacCi.Tac
				}
			}

			regState, err := modem3gpp.GetRegistrationState()
			var regStateStr string
			var isRoaming bool
			if err != nil {
				log.Println("error getting registration state:", err)
				regStateStr = "unknown"
				isRoaming = false
			} else {
				regStateStr = regState.String()
				isRoaming = regState == modemmanager.MmModem3gppRegistrationStateRoaming
			}

			state, err := modem.GetState()
			var stateStr string
			var isRegistered, isConnected bool
			if err != nil {
				log.Println("error getting modem state:", err)
				stateStr = "unknown"
				isRegistered = false
				isConnected = false
			} else {
				stateStr = state.String()
				isRegistered = state >= modemmanager.MmModemStateRegistered
				isConnected = state == modemmanager.MmModemStateConnected
			}

			opCode, err := modem3gpp.GetOperatorCode()
			var opCodeInt int64
			if err != nil {
				log.Println("error getting operator code:", err)
				opCodeInt = 0
			} else {
				if opCode == "" {
					opCodeInt = 0
				} else {
					if s, err := strconv.ParseInt(opCode, 10, 32); err == nil {
						opCodeInt = s
					} else {
						log.Println("error parsing operator code:", err)
						opCodeInt = 0
					}
				}
			}

			// Get message count
			msgCount := 0
			messaging, err := modem.GetMessaging()
			if err != nil {
				log.Println("error getting messaging:", err)
			} else {
				messages, err := messaging.List()
				if err != nil {
					log.Println("error listing messages:", err)
				} else {
					msgCount = len(messages)
				}
			}

			// Write single modem_status metric
			w.Write([]byte(fmt.Sprintf(
				"modem_status,%s power_state=\"%s\",cellid=\"%s\",lac=\"%s\",tac=\"%s\",registered=%t,connected=%t,state=\"%s\",operatorcode=%d,roaming=%t,regstate=\"%s\",signal_quality=%d,signal_quality_recent=%t,message_count=%d %d\n",
				tags, powerState.String(), cellID, lac, tac, isRegistered, isConnected, stateStr, opCodeInt, isRoaming, regStateStr, signalQuality, signalQualityRecent, msgCount, timestamp,
			)))

			// Signal metrics
			modemSignal, err := modem.GetSignal()
			if err != nil {
				log.Println("error getting modem signal:", err)
			} else {
				err = modemSignal.Setup(1)
				if err != nil {
					log.Println("error setting up modem signal:", err)
				} else {
					time.Sleep(400 * time.Millisecond)
					gsmSignal, err := modemSignal.GetGsm()
					if err != nil {
						log.Println("error getting gsm signal:", err)
					} else {
						signalType := gsmSignal.Type.String()
						signalTags := fmt.Sprintf("%s,signaltype=%s", tags, signalType)
						w.Write([]byte(fmt.Sprintf("modem_signal,%s rssi=%f,ber=%f %d\n", signalTags, gsmSignal.Rssi, gsmSignal.ErrorRate, timestamp)))
					}

					lteSignal, err := modemSignal.GetLte()
					if err != nil {
						log.Println("error getting lte signal:", err)
					} else {
						signalType := lteSignal.Type.String()
						signalTags := fmt.Sprintf("%s,signaltype=%s", tags, signalType)
						w.Write([]byte(fmt.Sprintf("modem_signal,%s rssi=%f,rsrp=%f,rsrq=%f,snr=%f,ber=%f %d\n", signalTags, lteSignal.Rssi, lteSignal.Rsrp, lteSignal.Rsrq, lteSignal.Snr, lteSignal.ErrorRate, timestamp)))
					}

					if err := modemSignal.Setup(0); err != nil {
						log.Println("error resetting modem signal setup:", err)
					}
				}
			}

			bearers, _ := modem.GetBearers()
			for _, bearer := range bearers {
				bearerType, err := bearer.GetBearerType()
				if err != nil {
					log.Println("error getting bearer type:", err)
					bearerType = modemmanager.MmBearerTypeUnknown
				}
				bearerTags := fmt.Sprintf("%s,bearer=%s", tags, bearerType.String())
				stats, err := bearer.GetStats()
				if err != nil {
					log.Println("error getting bearer stats:", err)
				} else {
					w.Write([]byte(fmt.Sprintf("modem_bearer_stats,%s rx_bytes=%d,tx_bytes=%d,duration=%d %d\n", bearerTags, stats.RxBytes, stats.TxBytes, stats.Duration, timestamp)))
				}

				bearerIpConfig, err := bearer.GetIp4Config()
				if err != nil {
					log.Println("error getting bearer ip config:", err)
				} else {
					w.Write([]byte(fmt.Sprintf("modem_bearer_ip_config,%s ip=\"%s\" %d\n", bearerTags, bearerIpConfig.Address, timestamp)))
				}

				bearerIp6Config, err := bearer.GetIp6Config()
				if err != nil {
					log.Println("error getting bearer ip6 config:", err)
				} else {
					w.Write([]byte(fmt.Sprintf("modem_bearer_ip6_config,%s ip=\"%s\" %d\n", bearerTags, bearerIp6Config.Address, timestamp)))
				}
			}

		}
	}
}

func main() {

	flag.Parse()

	mmgr, err := modemmanager.NewModemManager()
	if err != nil {
		log.Fatal(err.Error())
	}
	version, err := mmgr.GetVersion()
	if err != nil {
		log.Fatal(err.Error())
	}

	err = mmgr.SetLogging(modemmanager.MMLoggingLevelError)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Printf("Starting modem exporter using ModemManager v%s", version)

	exporter := NewExporter(mmgr)

	promRegistry := prometheus.NewRegistry()
	promRegistry.MustRegister(exporter)

	http.Handle(*metricsPath, promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{
		EnableOpenMetrics: false,
	}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Modem Exporter</title></head>
             <body>
             <h1>Modem Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             <p><a href='/influx'>Influx Line Protocol</a></p>
             </body>
             </html>`))
	})
	http.HandleFunc("/influx", influxHandler(mmgr))
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
