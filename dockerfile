#BUILD Go Lib
FROM golang:1.23 AS builder-go

# Set the working directory inside the container
WORKDIR /ProjectGo

ENV CGO_ENABLED=1
ENV GOOS=linux
ENV GOARCH=arm64
ENV CC=aarch64-linux-gnu-gcc

# Install build dependencies
RUN apt-get update && \
    apt-get install -y gcc-aarch64-linux-gnu

COPY ./go-code .

# Clean Go build cache
# Compile Go code to generate a shared library (.so) file
RUN go clean -cache -modcache -i -r && \
    go build -ldflags="-s -w" -buildmode=c-shared -o libgo.so chatp2p.go chatp2p_api.go

#BUILD DotNet
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS builder-dotnet

# Set the working directory inside the container
WORKDIR /ProjectDotNet

COPY ./main.cs .
COPY ./TelemetryLogger.cs .
COPY ./PeerPlayer.sln . 
COPY ./PeerPlayer.csproj . 

COPY --from=builder-go /ProjectGo/libgo.so ./

RUN dotnet restore && \
    dotnet publish --runtime linux-arm64 --self-contained -o out -p:DefineConstants=LINUX && \
    cp libgo.so ./out 

# Use the official .NET Docker image for ARM64 architecture
FROM mcr.microsoft.com/dotnet/aspnet:8.0 AS runtime

# Install necessary packages for GPIO access
RUN apt-get update && \
    apt-get install -y

# Set the working directory inside the container
WORKDIR /Project

# Copy the compiled binary into the container
COPY --from=builder-dotnet /ProjectDotNet/out .

# Make the binary executable
RUN chmod +x PeerPlayer

# Command to run the executable
ENTRYPOINT ["sh", "-c", "./PeerPlayer -bootstrapaddrs $RELAY_ADDRESS -iterations $NUM_ITERATION_SAMPLES -interval $TIME -test $TEST"]
