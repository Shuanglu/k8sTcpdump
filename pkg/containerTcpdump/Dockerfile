FROM jpetazzo/nsenter
RUN apt-get update -q && apt-get install coreutils tcpdump -y && mkdir /containerTcpdump
ADD ./containerTcpdump /containerTcpdump/
WORKDIR /containerTcpdump
ENTRYPOINT ["/bin/bash"]
CMD ["-c", "./containerTcpdump && tail -f /dev/null"]
