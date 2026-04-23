package capture

import "github.com/google/gopacket/pcap"

type Device struct {
	Name        string
	Description string
}

func Devices() ([]Device, error) {
	raws, err := pcap.FindAllDevs()
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(raws))
	for _, d := range raws {
		out = append(out, Device{Name: d.Name, Description: d.Description})
	}
	return out, nil
}
