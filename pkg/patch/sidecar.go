package patch

import (
	"bufio"
	"bytes"
	corev1 "k8s.io/api/core/v1"
	"text/template"
)

const envoyContainerTemplate = `
{
  "name": "envoy",
  "image": "{{ .ContainerImage }}",
  "securityContext": {
    "runAsUser": 1337
  },
  "ports": [
    {
      "containerPort": 9901,
      "name": "stats",
      "protocol": "TCP"
    }
  ],
  "env": [
    {
      "name": "APPMESH_VIRTUAL_NODE_NAME",
      "value": "mesh/{{ .MeshName }}/virtualNode/{{ .VirtualNodeName }}"
    },
    {
      "name": "APPMESH_PREVIEW",
      "value": "{{ .Preview }}"
    },
    {
      "name": "ENVOY_LOG_LEVEL",
      "value": "{{ .LogLevel }}"
    }{{ if or .EnableJaegerTracing .EnableDatadogTracing }},
    {
      "name": "ENVOY_STATS_CONFIG_FILE",
      "value": "/tmp/envoy/envoyconf.yaml"
    }{{ end }},
    {
      "name": "AWS_ROLE_SESSION_NAME",
      "value": "{{ .VirtualNodeName }}"
    },
    {
      "name": "AWS_REGION",
      "value": "{{ .Region }}"
    }{{ if .InjectXraySidecar }},
    {
      "name": "ENABLE_ENVOY_XRAY_TRACING",
      "value": "1"
    }{{ end }}{{ if .EnableStatsTags }},
    {
      "name": "ENABLE_ENVOY_STATS_TAGS",
      "value": "1"
    }{{ end }}{{ if .EnableStatsD }},
    {
      "name": "ENABLE_ENVOY_DOG_STATSD",
      "value": "1"
    }{{ end }}
  ],
  "volumeMounts": [
    {{ if .ServiceAccountVolumeMount }}{
      "mountPath": "{{ .ServiceAccountVolumeMount.MountPath }}",
      "name": "{{ .ServiceAccountVolumeMount.Name }}",
      "readOnly": {{ .ServiceAccountVolumeMount.ReadOnly }}
    }{{ end }}{{ if or .EnableJaegerTracing .EnableDatadogTracing }}{{ if .ServiceAccountVolumeMount }},{{ end }}
    {
      "mountPath": "/tmp/envoy",
      "name": "envoy-tracing-config"
    }{{ end }}
  ],
  "resources": {
    "requests": {
      "cpu": "{{ .CpuRequests }}",
      "memory": "{{ .MemoryRequests }}"
    }
  }
}
`
const xrayDaemonContainerTemplate = `
{
  "name": "xray-daemon",
  "image": "amazon/aws-xray-daemon",
  "securityContext": {
    "runAsUser": 1337
  },
  "ports": [
    {
      "containerPort": 2000,
      "name": "xray",
      "protocol": "UDP"
    }
  ],
  "env": [
    {
      "name": "AWS_ROLE_SESSION_NAME",
      "value": "{{ .VirtualNodeName }}"
    }
  ],
  "volumeMounts": [
    {{ if .ServiceAccountVolumeMount }}
    {
	  "mountPath": "{{ .ServiceAccountVolumeMount.MountPath }}",
	  "name": "{{ .ServiceAccountVolumeMount.Name }}",
	  "readOnly": {{ .ServiceAccountVolumeMount.ReadOnly }}
    }{{ end }}
  ],
  "resources": {
    "requests": {
      "cpu": "{{ .CpuRequests }}",
      "memory": "{{ .MemoryRequests }}"
    }
  }
}
`

type SidecarMeta struct {
	ContainerImage       string
	MeshName             string
	VirtualNodeName      string
	Preview              string
	LogLevel             string
	Region               string
	CpuRequests          string
	MemoryRequests       string
	EnableJaegerTracing  bool
	JaegerAddress        string
	JaegerPort           string
	EnableDatadogTracing bool
	DatadogAddress       string
	DatadogPort          string
	InjectXraySidecar    bool
	EnableStatsTags      bool
	EnableStatsD         bool

	//ServiceAccountVolumeMount is used to configure sidecars to use app container's SA.
	//This is necessary to allow envoy-proxy, x-ray-daemon and other sidecars to use IRSA to get AWS credentials.
	ServiceAccountVolumeMount *corev1.VolumeMount
}

func renderSidecars(meta SidecarMeta) ([]string, error) {
	var sidecars []string

	envoySidecar, err := renderTemplate("envoy", envoyContainerTemplate, meta)
	if err != nil {
		return sidecars, err
	}

	sidecars = append(sidecars, envoySidecar)

	if meta.InjectXraySidecar {
		xrayDaemonSidecar, err := renderTemplate("xray-daemon", xrayDaemonContainerTemplate, meta)
		if err != nil {
			return sidecars, err
		}

		sidecars = append(sidecars, xrayDaemonSidecar)
	}

	return sidecars, nil
}

func renderTemplate(name string, t string, meta SidecarMeta) (string, error) {
	tmpl, err := template.New(name).Parse(t)
	if err != nil {
		return "", err
	}
	var data bytes.Buffer
	b := bufio.NewWriter(&data)

	if err := tmpl.Execute(b, meta); err != nil {
		return "", err
	}
	err = b.Flush()
	if err != nil {
		return "", err
	}
	return data.String(), nil
}
