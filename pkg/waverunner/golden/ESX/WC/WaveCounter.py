#!/usr/bin/env python3
###############################################################################
# Copyright (c) 2018 VMware, Inc.  All rights reserved. -- VMware Confidential
###############################################################################
# Scirpt functions as a wrapper around vmkperf for improved usability.

__author__ = 'VMware, Inc.'


##########
# imports
##########


import os
import sys
import time
import argparse
import subprocess
import multiprocessing
import platform
import shlex
import threading
import signal
try:
    import Queue as queue # Python 2.7 Usage
except ImportError:
    import queue # Python 3.x Usage
from random import randint


##############################
# module variable definitions
##############################


# Help Message displayed when no command line arguments are given, indicating
# how to use this script. Additionally, when an incorrect option is specified,
# the script will display a usage message.
USAGE_MESSAGE = './WaveCounter.py -h [hostname] -p [password] -r [runtag] -w [source] -s -[cuo] [event] -f [file] -i [time] -n [integer] -t [time] -l[+]'
HELP_MESSAGE = ('usage: %s\n\n'
                '       Core Event (-c [event]) - e:[0xAA],u:[0xBB],d:[ukie],c:[0xCC]\n\n'
                '                  eventSel => e:[0xAA]     format: 0xAA\n'
                '                  unitMask => u:[0xBB]     format: 0xBB\n'
                '                 modifiers => d:[ukei]\n'
                '                                "u" (User)\n'
                '                                "k" (Kernal)\n'
                '                                "e" (Edge Detect)\n'
                '                                "i" (Invert)\n'
                '               counterMask => e:[0xCC]     format: 0xCC\n\n'
                '     Uncore Event (-u [event]) - Currently Unsupported\n\n'
                '    Offcore Event (-o [event]) - Currently Unsupported\n\n'
                'Options:\n'
                '----------\n'
                '-h          | ESX IP / Hostnames, given as a comma seperated list (No Spaces)  format: [IP_1],[IP_2],...[IP_N]\n'
                '-p          | Universal Password for all ESX IP / Hostnames\n'
                '-r          | Runtag - Required Tag for improved Data Identification in Wavefront\n'
                '-w          | Source - Indicate the Source / Name of the Data when sent to Wavefront\n'
                '-s          | Stdout Toggle for Data Stream   [Given - Show Data Stream / Not Given - No Data Stream]\n'
                '-c          | Core PMU Events\n'
                '-u          | Uncore PMU Events\n'
                '-o          | Offcore PMU Events\n'
                '-f          | File input, containing a series of Core, Uncore, and Offcore Events\n'
                '-i          | Sampling Interval                  format: [integer][SMH]\n'
                '                                                         (Note: Time Unit indicator is OPTIONAL)\n'
                '                                                         "S" - Seconds\n'
                '                                                         "M" - Minutes\n'
                '                                                         "H" - Hours\n'
                '-t          | Amount of time to make Samples     format: [integer][SMH]\n'
                '                                                         (Note: Time Unit indicator is OPTIONAL)\n'
                '                                                         "S" - Seconds\n'
                '                                                         "M" - Minutes\n'
                '                                                         "H" - Hours\n'
                '-n          | Number of Samples to Take          format: [integer]\n'
                '-l          | Brief Descriptions of All PMU Events and Errata\n'
                '-l+         | Detailed Information of ALL PMU Events\n'
                % (USAGE_MESSAGE))

# Template for SSHing into a machine.
SSH_TEMPLATE = "sshpass -p {0} ssh -o LogLevel=quiet -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no root@{1}"

# Necessary files for installing Wavefront and obtaining PMC data.
WAVEFRONT_INSTALLATION_SCRIPT = "install_configure_wavefront.sh"
WAVEFRONT_INSTALLATION_DEPENDENCY_1 = "telegraf.conf"
INTERNAL_SCRIPT = "perfcounters.py"

# Supported operating systems of this script / Wavefront.
SUPPORTED_OS = [ "Linux",
               ]


##############################
# module function definitions
##############################


# Handles SIGTERM and SIGINT signals sent to the program for a clean exit.
# @param: signal - Signal Number of Interest
# @param: frame - Interpreted Stack frame
# @throw: SignalInterupt - Function's sole intent is to raise the Exception
def handleStopSignal(signal, frame):
    """Throws an Exception when a terminating signal is sent to the program."""

    raise SignalInterupt

    return


# Acquires command line arguments.
# @return: Dictionary of command line options mapped to their arguments.
def getArgs():
    """Obtains command line arguments for interfacing with vmkperf from
    specified servers."""

    # Establishes the argument parser.
    parser = argparse.ArgumentParser(add_help=False,
                                     usage=USAGE_MESSAGE)

    # Determines if any command line arguments have been given. If none are
    # given, a help message displaying the scripts usage is displayed.
    if len(sys.argv) is 1:
        print(HELP_MESSAGE)
        sys.exit(0)

    # Declares all command line options for interfacing with this script, in
    # addition to the internal script on ESX servers ("-h" option) for
    # interfacing with vmkperf.

    # Pertinent WaveCounter command line arguments.
    parser.add_argument('-h', # Comma seperated list of hostnames
                        action='append',
                        dest='hostname',
                        required=True,
                        default=[]
                       )
    parser.add_argument('-p', # Password for all provided hostnames
                        action='append',
                        dest='password',
                        required=True,
                        default=[]
                       )
    parser.add_argument('-r', # Runtag of the PMC data
                        action='append',
                        dest='runtag',
                        required=True,
                        default=[]
                       )
    parser.add_argument('-w', # Identifies Source of PMU data
                        action='append',
                        dest='source',
                        required=True,
                        default=[]
                        ),
    parser.add_argument('-s', # Dump data to stdout toggle
                        action='store_true',
                        dest='stdoutToggle',
                        required=False,
                        default=False
                       )

    # Pertinent internal script command line arguments.
    parser.add_argument('-c', # Core PMU Events
                        action='append',
                        dest='core',
                        required=False,
                        default=[]
                       )
    parser.add_argument('-u', # Uncore PMU Events
                        action='append',
                        dest='uncore',
                        required=False,
                        default=[]
                       )
    parser.add_argument('-o', # Offcore PMU Events
                        action='append',
                        dest='offcore',
                        required=False,
                        default=[]
                       )
    parser.add_argument('-i', # Sampling interval
                        action='append',
                        dest='interval',
                        required=False,
                        default=[]
                       )
    parser.add_argument('-n', # Number of Samples to take
                        action='append',
                        dest='numSamples',
                        required=False,
                        default=[]
                       )
    parser.add_argument('-t', # Time in which to collect samples
                        action='append',
                        dest='time',
                        required=False,
                        default=[]
                       )
    parser.add_argument('-f', # File containing PMU events
                        action='append',
                        dest='fileInput',
                        required=False,
                        default=[]
                       )
    parser.add_argument('-l', # Bried Desctiption Toggle
                        action='store_true',
                        dest='briefDescriptionToggle',
                        required=False,
                        default=False
                        )
    parser.add_argument('-l+', # Detailed Description Toggle
                        action='store_true',
                        dest='detailedDescriptionToggle',
                        required=False,
                        default=False
                        )

    # Converts the command line options and corresponding arguments into a
    # dictionary.
    return vars(parser.parse_args())


# Verifies the validity of all provided command line arguments.
# @param: args - Dictionary of command line arguments
# @return: TRUE if all WaveCounter arguments are valid; otherwise, FALSE
def verifyArgs(args):
    """Validates all command line arguments intended for WaveCounter."""

    # Determines if more than one argument was given for the password ("-t"
    # option), runtag ("-r" option), or source ("-w" option).
    for arg in ["password", "runtag", 'source']:

        # Indicates the corresponding command line option.
        if arg == 'password':
            flag = '-p'
        elif arg == 'runtag':
            flag = '-r'
        else:
            flag = '-w'

        # Determines if multiple arguments were given for a command line
        # option.
        if len(args[arg]) > 1:
            sys.stderr.write('Warning: Multiple arguments given to the "%s" '
                             'option.\n'
                             '         Arguments:\n' % (arg, flag))
            message =  '               %s <-- First Argument\n'
            for item in args[arg]:
                sys.stderr.write(message % (item))
                message = '               %s\n'
            sys.stderr.write("         Only the first argument will be used.\n")

        # Converts an empty string into an "empty string" accepted by a bash terminal.
        if args[arg][0] == "":
            args[arg] = '""'
        else:
            args[arg] = args[arg][0]

    # Determines if more than one batch of hostnames was given. A "batch"
    # signifies a comma seperated list of hostnames. If more than one batch
    # is given, only the first batch will be used.
    if len(args['hostname']) is not 1:
        sys.stderr.write('Warning: Expected comma seperated list of hostnames. Only\n'
                         '         the first batch of hostnames will be used.\n'
                         '         First Batch:\n'
                         '                %s\n' % (args['hostname'][0]))
    args['hostname'] = args['hostname'][0]

    # Parses the comma seperated list of hostnames into an array.
    temp = []
    for host in args['hostname'].split(','):
        temp.append(host)
    args['hostname'] = temp

    return True


# Formats and sends PMU data to Wavefront.
# @param: hostname - ESX Server name / IP address
# @param: runtag - Identifier tag of all PMU data send to Wavefront
# @param: source - Location name of all PMU data in Wavefront
# @param: data - Raw PMU data obtained from the internal script
def send2Wavefront(hostname, runtag, source, data):
    """Formats and sends performance counter data to Wavefront."""

    # Parses the raw PMU data sent from the internal script.
    rawData = data.split(',')

    times = '%s000000000' % (rawData[0]) # Time stamp, in nanoseconds
    # Name/Identifier of processed PMU Event, replacing the "_" with "-" in
    # order to not concflict with Wavefront's metric hierarchy.
    name = rawData[2].replace('_', '-')
    pcpuData = rawData[3:] # PMU data from all pCPUs

    # Initializes list of subprocesses for processing data from each
    # pCPU core on the server.
    processCPUData = [None] * len(pcpuData)

    # Processes data from all pCPUs on the server, and sends corresponding
    # tags and data to Wavefront.
    with open(os.devnull, 'wb') as devnull:
        cmdTemplate = ('echo "esx.vmkperf,runtag=%s,source=%s,pCPU=%s,'
                       'ESXIP=%s %s=%s %s" | socat -t 0 - UDP:localhost:8094')
        for i, j in enumerate(pcpuData):
            cmd = cmdTemplate % (runtag,
                                 source,
                                 i,
                                 hostname,
                                 name,
                                 j,
                                 times)
            # Starts a process to send PMU pCPU data to Wavefront.
            processCPUData[i] = subprocess.Popen(cmd,
                                                shell=True,
                                                stdout=devnull,
                                                stderr=devnull,
                                                close_fds=True)

    return


# Terminates all running vmkperf Events.
# @param: hostname - ESX Server name / IP address
# @param: password - Password for provided ESX Server name / IP Address
def killRunningEvents(hostname, password):
    """Terminates all running vmkperf events from the provided server."""

    # Pauses execution of the following commands to ensure the the internal
    # python script used to obtain the PMU data termiantes properly.
    time.sleep(1)

    # Command to ssh into the server and stop all vmkperf events.
    cmdTemplate = SSH_TEMPLATE + ' "vmkperf stop all"'
    cmd = cmdTemplate.format(password, hostname)

    # Starts a process to ssh into the provided server and execute a command
    # to terminate all running vmkperf Events.
    with open(os.devnull, 'wb') as devnull:
        try:
            subprocess.check_call(shlex.split(cmd),
                                  stdout=devnull,
                                  stderr=devnull)
        except subprocess.CalledProcessError:
            sys.stderr.write('Error: Unable stop all vmkperf events from '
                             'running on host "%s".\n'
                             % (hostname))
    return


# Sends a file to the /tmp/ directory of the provided server.
# @param: hostname - ESX Server name / IP address
# @param: password - Password for provided ESX Server name / IP Address
# @param: filename - Name of the file to be sent to the server
# @return: TRUE if the file was successfully transferred; otherwise, FALSE
def sendFile2Server(hostname, password, filename):
    """Transfers a file to the /tmp/ directory of the provided server."""

    # Command to enable file transfer to the server.
    cmdTemplate = ('sshpass -p {0} scp -o LogLevel=quiet -o '
                   'UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no '
                   '{1} root@{2}:/tmp/')
    cmd = cmdTemplate.format(password, filename, hostname)

    # Starts a process to transfer a file from the user's local machine
    # to the provided server.
    with open(os.devnull, 'wb') as devnull:
        try:
            subprocess.check_call(shlex.split(cmd),
                                  stdout=devnull,
                                  stderr=devnull)
        except subprocess.CalledProcessError:
            sys.stderr.write('Error: Unable to send "%s" to host "%s".\n'
                             % (filename, hostname))
            return False

    return True


# Locates files on the user's machine.
# @param: filename - Name of the desired file
# @param: path - Base Directory to start search
# @return: Path to the file if found; otherwise, None
def findFile(filename, path):
    """Locates the first directory containing the desired filename."""
    try:
        for root, dirs, files in os.walk(path):
            if filename in files:
                return os.path.join(root, filename)
    except OSError:
        pass

    return None


# Removes previously transferred PMU Event files from the server.
# @param: hostname - ESX Server name / IP address
# @param: password - Password for provided ESX Server name / IP Address
# @param: fileList - List of previously transferred PMU Event files
def removeEventFiles(hostname, password, fileList):
    """Removes previously transferred PMU Event files from the server."""
    
    # Template to ssh into the provided server.
    ssh = SSH_TEMPLATE.format(password, hostname)
    # For every file in the list, a process is started to ssh into the
    # provided server and remove the previously transferred file.
    for aFile in fileList:
        cmd = '%s "rm /tmp/%s"' % (ssh, aFile)
        with open(os.devnull, 'wb') as devnull:
            try:

                subprocess.check_call(shlex.split(cmd),
                                      stdout=devnull,
                                      stderr=devnull)
            except (subprocess.CalledProcessError, SignalInterupt):
                sys.stderr.write('Error: Unable to remove "%s" from '
                                 'host "%s".\n'
                                 % (aFile, hostname))
    return


# Locates the internal script on the server; otherwise, transfers a copy.
# @param: hostname - ESX Server name / IP address
# @param: password - Password for provided ESX Server name / IP Address
# @return: TRUE if internal script transferred; otherwise, FALSE
def locateInternalScript(hostname, password):
    """Locate the internal script on server; otherwise, transferrs a copy."""

    # Starts a process to locate the internal script in the /tmp/
    # directory of the provided server.
    ssh = SSH_TEMPLATE.format(password, hostname)
    cmd = '{0} "ls /tmp/ | grep -c {1}"'.format(ssh, INTERNAL_SCRIPT)
    process = subprocess.Popen(shlex.split(cmd),
                               stdout=subprocess.PIPE,
                               bufsize=1)
    stdout, _ = process.communicate()

    # The internal script is located in the /tmp/ directory.
    if stdout.decode('ascii').strip() == '1':
        return True

    # Locates the internal script on the user's machine.
    internal = findFile(INTERNAL_SCRIPT, '.')
    if internal is None:
        sys.stderr.write('Error: Could not locate the internal script: "%s"'
                         % (INTERNAL_SCRIPT))
        return False
    # Transfers the internal script to the server if the script is not located
    # in the /tmp/ directory of the server.
    else:
        if sendFile2Server(hostname, password, internal) is False:
            sys.stderr.write('Error: Failed to transfer "%s" to\n'
                             '       the hostname: %s\n'
                             % (INTERNAL_SCRIPT, hostname))
            return False

    return True


# Enables non-blocking reading of stdout / stderr from a subprocess.
# @param: process - I/O stream from a subprocess
# @param: outQueue - Queue of lines from an I/O stream
def output_reader(process, outQueue):
    """Enables non-blocking reading of stdout/stderr from a subprocess."""

    # Processes each line present from an I/O stream, storing the line
    # in a Queue for later processing.
    for line in iter(process.readline, b''):
        outQueue.put(line.decode('utf-8'))
        process.flush()
    process.close()

    return


# Executes the internal script for obtaining PMU data from the provided server.
# @param: args - Dictionary of command line arguments
# @return: TRUE if data collection was successful; otherwise, FALSE
def executeVMKPerf(args):
    """Executes the internal script on the server for processing PMU data."""

    # Ensure's graceful exit upon recieving a SIGINT or SIGTERM signal.
    signal.signal(signal.SIGINT, handleStopSignal)
    signal.signal(signal.SIGTERM, handleStopSignal)

    # Information necessary for WaveCounter to function.
    hostname = args["hostname"]
    password = args["password"]
    dumpStdout = args["stdoutToggle"]
    runtag = args["runtag"]
    source = args['source']

    ssh = SSH_TEMPLATE.format(password, hostname)

    try:

        # Verifies the existance of the internal script on the provided
        # server. If the internal script cannot be transferred onto the
        # server, the data collection cannot proceed.
        if locateInternalScript(hostname, password) is False:
            return False

        # Appends the corresponding PMU Events given via the command line
        # to the internal script's command line arguments.
        scriptPlusArgs = INTERNAL_SCRIPT
        for i in args["core"]:
            scriptPlusArgs = "{0} -c {1}".format(scriptPlusArgs, i)
        for i in args["uncore"]:
            scriptPlusArgs = "{0} -u {1}".format(scriptPlusArgs, i)
        for i in args["offcore"]:
            scriptPlusArgs = "{0} -o {1}".format(scriptPlusArgs, i)

        # Appends all other command line arguments to the internal script.
        lEnabled = args["briefDescriptionToggle"]
        lPlusEnabled = args["detailedDescriptionToggle"]
        usedFiles = []
        for i in args["fileInput"]:
            if os.path.isdir(i) or os.path.isfile(i):
                theFile = (os.path.split(i))[-1]
                if sendFile2Server(hostname, password, i):
                    usedFiles.append(theFile)
                    scriptPlusArgs = "{0} -f {1}".format(scriptPlusArgs, theFile)
            else:
                sys.stderr.write("Error: Could not locate the file \'{0}\' in the current directory: \'{1}\'\n".format(i, os.getcwd()))
        for i in args["interval"]:
            scriptPlusArgs = "{0} -i {1}".format(scriptPlusArgs, i)
        for i in args["numSamples"]:
            scriptPlusArgs = "{0} -n {1}".format(scriptPlusArgs, i)
        for i in args["time"]:
            scriptPlusArgs = "{0} -t {1}".format(scriptPlusArgs, i)
        if args["briefDescriptionToggle"]:
            scriptPlusArgs = "{} -l".format(scriptPlusArgs)
        if args["detailedDescriptionToggle"]:
            scriptPlusArgs = "{} -l+".format(scriptPlusArgs)

        noInternalArgs = True
        if scriptPlusArgs != INTERNAL_SCRIPT:
            noInternalArgs = False

        # Invokes a process on the server to launch the internal script to
        # obtain PMU data from vmkperf.
        ON_POSIX = 'posix' in sys.builtin_module_names
        cmd = '{0} "/tmp/{1}"'.format(ssh, scriptPlusArgs)
        process = subprocess.Popen(shlex.split(cmd),
                                   stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE,
                                   bufsize=1,
                                   close_fds=ON_POSIX)

        # Setups a queue for non-blocking reads from stdout and stderr using
        # seperated threads. This is done to prevent stdout or stderr reads
        # from hanging indefinitely if stdout or stderr is empty when
        # attempting to read it's input.
        outq = queue.Queue()
        t = threading.Thread(target=output_reader,
                             args=(process.stdout, outq))
        t.daemon = True # Threads end when the subprocess ends
        t.start()
        outp = queue.Queue()
        g = threading.Thread(target=output_reader,
                             args=(process.stderr, outp))
        g.daemon = True # Threads end when the subprocess ends
        g.start()

        internalPID = None

        # Gathers stdout and stderr from the internal script, piping the output
        # to a terminal window, if requested.
        while True:
            exitStatus = process.poll()
            output = None
            myerr = None

            # Reads a line from the stdout queue
            try:
                while True:
                    if internalPID is None:
                        internalPID = (outp.get_nowait().split())[1]
                    else:
                        sys.stderr.write(outp.get_nowait())
            except queue.Empty:
                pass

            # Reads a line from the stderr queue
            try:
                output = outq.get_nowait()
            except queue.Empty:
                pass
            else:
                # Determines if "-l" or "-l+" options were given. If not, then
                # all output to stdout is PMU data.
                if not lEnabled and not lPlusEnabled and not noInternalArgs:
                    send2Wavefront(hostname, runtag, source, output)
                # Dumps stdout to terminal if requested, to view data stream.
                if dumpStdout:
                    print(output.strip('\n'))

            # Determines if process has ended and
            if output is None and myerr is None and exitStatus is not None:
                break

    # Handles the invocation of an abrupt exit (ctrl-c) gracefully by
    # terminating all currently running vmkperf Events on the server.
    except (KeyboardInterrupt, SignalInterupt):
        # Terminates the process of invokeing the internal script.
        try:
            process.terminate()
        except NameError:
            pass

    # Cleanup by invoked process of internal script by ending the stdout
    # and stderr queues, terminating the process, and removing all
    # transferred PMU Event files from the server.
    finally:

        # Disables handling all termination signals sent to this script, in
        # order to ensure a graceful exit.
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)

        print('\nWaveCounter Shutdown - Host "%s": Initiated.' % (hostname))
        killCMD = '%s "kill -9 %s"' % (ssh, internalPID)
        try:
            killInternal = subprocess.Popen(shlex.split(killCMD),
                                            stdout=subprocess.DEVNULL,
                                            stderr=subprocess.DEVNULL,
                                           )
        except subprocess.CalledProcessError:
            sys.stderr.write('Could not terminate internal script - Host "%s": Failed' % (hostname))
        else:
            # Removes all transfered PMU Event files from the server.
            removeEventFiles(hostname, password, usedFiles)
            # Stops all currently running vmkperf Events on the provided host.
            killRunningEvents(hostname, password)
            print('WaveCounter Shutdown - Host "%s": Completed.' % (hostname))

    return


# Installs Wavefront client, and accompanying dependencies onto the machine.
# @return: TRUE if Wavefront successfully installed; otherwise, FALSE
def installWavefront():
    """Installs the Wavefront client, and accompanying dependencies."""

    # Verifies that the Wavefront installation script is present on the user's
    # machine.
    internal = findFile(WAVEFRONT_INSTALLATION_SCRIPT, '.')
    if internal is None:
        sys.stderr.write(("Error: Could not locate the Wavefront installation " +
                          "script: \'{0}\'\n").format(WAVEFRONT_INSTALLATION_SCRIPT))
        return False

    # The "telegraf.conf" file must also be in the same directory as the Wavefront
    # installation script in order to proceed.
    directory = os.path.dirname(internal)
    dependency = findFile(WAVEFRONT_INSTALLATION_DEPENDENCY_1, directory)
    if dependency is None:
        sys.stderr.write(("Error: Could not locate the Wavefront installation " +
                          "dependency: \'{0}\'\n").format(WAVEFRONT_INSTALLATION_DEPENDENCY_1))
        return False

    # Invokes the Wavefront installation script to install Wavefront.
    process = subprocess.Popen([internal],
                               stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE,
                               bufsize=1)
    _, stderr = process.communicate()

    # Determines if the Wavefront installation was successful. If not, then the
    # associated error messages associated with the attempt are displayed.
    if process.returncode is not 0:
        sys.stderr.write(stderr.decode("ascii"))
        return False

    return True


###########################
# module class definitions
###########################


# Indicates that a SIGINT or a SIGTERM signal has been sent to the program.
class SignalInterupt(Exception):
    pass


##############
# module code
##############


if __name__ == "__main__":

    # Verifies that the host's OS is suppored for use with Wavefront.
    sysOS = platform.system()
    if sysOS not in SUPPORTED_OS:
        sys.stderr.write('Error: "%s" is not a currently supported OS for\n'
                         '       wiring data to Wavefront.\n'
                         % (sysOS))
        sys.exit(1)
    else:
        # Obtains and validates all provided command line arguments. If any
        # invalid arguments were given, the program ends.
        args = getArgs()
        if not verifyArgs(args):
            sys.exit(1)
        if not installWavefront():
            sys.exit(1)


    # Parses all command line arguments into a series of dictionaries, each
    # containing a distinct ESX Server IP / Name. This is done for ease of
    # use with the multiprocessing "map_async", which takes a function as an
    # argument, and a single argument for that function.
    sessionArgs = []
    for i in args['hostname']:
        temp = args.copy()
        temp['hostname'] = i
        sessionArgs.append(temp)

    # Temporarily prevents SIGINT and SIGTERM signals from being registered
    # by the script. This is done to allow signal communication between
    # newly spawned processes while using the multiprocessing module.
    int_sigHandler = signal.signal(signal.SIGINT, signal.SIG_IGN)
    term_sigHandler = signal.signal(signal.SIGTERM, signal.SIG_IGN)

    # Establishes an object to control a group of newly spawned processes, which
    # is equivalent to the number of hostnames provided.
    session = multiprocessing.Pool(processes=len(sessionArgs))

    # Re-enables the detection of SIGINT and SIGTERM by the script.
    signal.signal(signal.SIGINT, int_sigHandler)
    signal.signal(signal.SIGTERM, term_sigHandler)

    # Ensure's graceful exit upon recieving a SIGINT or SIGTERM signal.
    signal.signal(signal.SIGINT, handleStopSignal)
    signal.signal(signal.SIGTERM, handleStopSignal)

    try:
        # Invokes PMU data collection from the provided list of ESX Server IPs /
        # Namess in parallel. Data is formatted and sent to Wavefront.
        session.map(executeVMKPerf, sessionArgs)

    # Handles ctrl-c interupt gracefully; stops and terminates all PMU data
    # collection processes.
    except (KeyboardInterrupt, SignalInterupt):
        try:
            session.close()
        except NameError:
            pass
        else:
            session.join()
    else:
        session.close()
        session.join()
