FROM ubuntu:13.10

ADD ./msc-thesis-docker-cpu-monitor /msc-thesis-docker-cpu-monitor
ENTRYPOINT ["/msc-thesis-docker-cpu-monitor"]
